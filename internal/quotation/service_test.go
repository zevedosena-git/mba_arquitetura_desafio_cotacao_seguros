package quotation

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/partner"
	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/platform"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var threePartners = []platform.Partner{
	{Name: "partner-slow", BaseURL: "http://slow"},
	{Name: "partner-flaky", BaseURL: "http://flaky"},
	{Name: "partner-degrading", BaseURL: "http://degrading"},
}

func validRequest() Request {
	r := Request{
		Driver:  Driver{Document: "12345678901", BirthYear: 1988},
		Vehicle: Vehicle{Plate: "ABC1D23", Model: "Gol 1.0", Year: 2020, ValueCents: 8500000},
	}
	if err := r.Normalize(); err != nil {
		panic(err)
	}
	return r
}

type fakeQuoter struct {
	delay      time.Duration
	premiums   map[string]int64
	failOn     string
	failAll    bool
	calls      []string
	requests   []any
	concurrent atomic.Int32
	peak       atomic.Int32
}

func (f *fakeQuoter) Quote(_ context.Context, p platform.Partner, request any) (partner.Quote, error) {
	if now := f.concurrent.Add(1); now > f.peak.Load() {
		f.peak.Store(now)
	}
	defer f.concurrent.Add(-1)

	f.calls = append(f.calls, p.Name)
	f.requests = append(f.requests, request)
	time.Sleep(f.delay)

	if f.failAll || p.Name == f.failOn {
		return partner.Quote{}, &partner.Error{Partner: p.Name, Status: 503, Reason: "partner answered 503"}
	}
	return partner.Quote{Partner: p.Name, PremiumCents: f.premiums[p.Name], Currency: "BRL"}, nil
}

func defaultPremiums() map[string]int64 {
	return map[string]int64{"partner-slow": 180000, "partner-flaky": 90000, "partner-degrading": 120000}
}

func TestQuoteAggregatesTheThreePartnersSortedByPremium(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums()}
	response, err := NewService(threePartners, quoter, nil).Quote(context.Background(), "corretora-a", validRequest())
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if len(response.Quotes) != 3 {
		t.Fatalf("%d quotes, expected 3", len(response.Quotes))
	}
	expected := []string{"partner-flaky", "partner-degrading", "partner-slow"}
	for i, name := range expected {
		if response.Quotes[i].Partner != name {
			t.Errorf("quote %d is from %q, expected from %q", i, response.Quotes[i].Partner, name)
		}
	}
	if response.TenantID != "corretora-a" {
		t.Errorf("tenant_id %q, expected corretora-a", response.TenantID)
	}
}

// Motivo: cache nil mantém o laço em série e o teste que já existia. A paralelização é bônus e não foi feita. Atende docs/enunciado.md, Entrega 2 item 5 e a seção Bônus.
func TestQuoteCallsThePartnersSerially(t *testing.T) {
	quoter := &fakeQuoter{delay: 40 * time.Millisecond, premiums: defaultPremiums()}

	start := time.Now()
	if _, err := NewService(threePartners, quoter, nil).Quote(context.Background(), "corretora-a", validRequest()); err != nil {
		t.Fatalf("Quote: %v", err)
	}
	elapsed := time.Since(start)

	if minimum := 3 * quoter.delay; elapsed < minimum {
		t.Fatalf("aggregation took %s; serially it should take at least %s", elapsed, minimum)
	}
	if peak := quoter.peak.Load(); peak != 1 {
		t.Fatalf("%d concurrent calls at the peak, expected 1 (serial calls)", peak)
	}
}

// Motivo: o teste antigo esperava 502 na primeira falha. Agora espera 200 com as propostas que chegaram e partner-flaky em missing_partners. Atende docs/enunciado.md, Entrega 2 item 3 (Fallback: resposta parcial).
func TestOnePartnerDownBringsDownTheWholeRequest(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums(), failOn: "partner-flaky"}

	response, err := NewService(threePartners, quoter, nil).Quote(context.Background(), "corretora-a", validRequest())
	if err != nil {
		t.Fatalf("one partner down aborted the quote: %v", err)
	}
	if !response.Degraded || response.ServedFromCache || response.CacheAgeSeconds != nil {
		t.Fatalf("partial quote flags: %+v", response)
	}
	if len(response.Quotes) != 2 {
		t.Fatalf("%d quotes, want the two partners that answered", len(response.Quotes))
	}
	if len(response.MissingPartners) != 1 || response.MissingPartners[0] != "partner-flaky" {
		t.Fatalf("missing_partners %v, want partner-flaky", response.MissingPartners)
	}
	if len(quoter.calls) != 3 {
		t.Fatalf("calls %v, want the loop to continue after partner-flaky", quoter.calls)
	}
	if response.Quotes[0].PremiumCents > response.Quotes[1].PremiumCents {
		t.Fatalf("quotes are not ordered by premium: %+v", response.Quotes)
	}
}

func TestBrokerGoesInThePartnerRequest(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums()}
	service := NewService(threePartners[:1], quoter, nil)

	if _, err := service.Quote(context.Background(), "corretora-a", validRequest()); err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if _, err := service.Quote(context.Background(), "corretora-b", validRequest()); err != nil {
		t.Fatalf("Quote: %v", err)
	}

	toA, toB := serialize(t, quoter.requests[0]), serialize(t, quoter.requests[1])
	if toA == toB {
		t.Fatalf("different brokers produced the same request to the partner: %s", toA)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(toA), &body); err != nil {
		t.Fatalf("request to the partner is not JSON: %v", err)
	}
	if body["broker"] != "corretora-a" {
		t.Errorf("broker %v, expected corretora-a", body["broker"])
	}
	if _, ok := body["vehicle"]; !ok {
		t.Errorf("request to the partner does not carry the vehicle: %s", toA)
	}
}

// Motivo: a segunda chamada idêntica, com a placa em minúsculas, não chama o Quoter e marca quotation.served_from_cache. Atende docs/enunciado.md, Entrega 2 itens 2 e 4.
func TestSecondIdenticalCallIsServedFromCache(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousTracer := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() { otel.SetTracerProvider(previousTracer) })

	quoter := &fakeQuoter{premiums: defaultPremiums()}
	service := NewService(threePartners, quoter, NewMemoryCache(300*time.Second, time.Now))
	request := validRequest()

	first, err := service.Quote(context.Background(), "corretora-a", request)
	if err != nil {
		t.Fatalf("first Quote: %v", err)
	}
	if first.ServedFromCache || first.Degraded || len(first.Quotes) != 3 {
		t.Fatalf("first response is not a live complete quote: %+v", first)
	}

	ctx, span := otel.Tracer("test").Start(context.Background(), "POST /quotes")
	lower := request
	lower.Vehicle.Plate = "abc1d23"
	second, err := service.Quote(ctx, "corretora-a", lower)
	span.End()
	if err != nil {
		t.Fatalf("second Quote: %v", err)
	}
	if len(quoter.calls) != 3 {
		t.Fatalf("cache hit called the partners again: %v", quoter.calls)
	}
	if !second.ServedFromCache || second.Degraded || second.CacheAgeSeconds == nil || len(second.MissingPartners) != 0 {
		t.Fatalf("hit contract: %+v", second)
	}
	if len(second.Quotes) != 3 || second.Quotes[0].PremiumCents > second.Quotes[1].PremiumCents {
		t.Fatalf("hit quotes: %+v", second.Quotes)
	}

	marked := false
	for _, ended := range recorder.Ended() {
		for _, kv := range ended.Attributes() {
			if kv.Key == "quotation.served_from_cache" && kv.Value.AsBool() {
				marked = true
			}
		}
	}
	if !marked {
		t.Fatal("hit span is missing quotation.served_from_cache=true")
	}

	hits, misses := lookupCounts(t, reader)
	if hits != 1 || misses != 1 {
		t.Fatalf("cache lookups hit=%d miss=%d, want 1 and 1", hits, misses)
	}
}

// Motivo: resposta com parceira faltando não é gravada, então a próxima chamada volta às parceiras. Atende docs/enunciado.md, Entrega 2 item 2: o que acontece com a entrada quando o breaker abre.
func TestPartialQuoteIsNotCached(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums(), failOn: "partner-flaky"}
	service := NewService(threePartners, quoter, NewMemoryCache(300*time.Second, time.Now))
	request := validRequest()
	if _, err := service.Quote(context.Background(), "corretora-a", request); err != nil {
		t.Fatalf("partial Quote: %v", err)
	}
	if _, err := service.Quote(context.Background(), "corretora-a", request); err != nil {
		t.Fatalf("repeat Quote: %v", err)
	}
	if len(quoter.calls) != 6 {
		t.Fatalf("partial response was reused from cache: %d calls, want 6", len(quoter.calls))
	}
}

func lookupCounts(t *testing.T, reader *sdkmetric.ManualReader) (hits, misses int64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "quotation_cache_lookups_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("lookup data type %T", m.Data)
			}
			for _, point := range sum.DataPoints {
				value, found := point.Attributes.Value(attribute.Key("result"))
				if !found {
					continue
				}
				switch value.AsString() {
				case "hit":
					hits += point.Value
				case "miss":
					misses += point.Value
				}
			}
		}
	}
	return hits, misses
}

func serialize(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}
