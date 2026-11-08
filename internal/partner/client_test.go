package partner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/platform"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func testPartner(h http.Handler) (platform.Partner, func()) {
	server := httptest.NewServer(h)
	return platform.Partner{Name: "partner-flaky", BaseURL: server.URL}, server.Close
}

// Motivo: NewClient passa a receber Options. Options{} usa o default de produção, então uma falha não abre e os testes que já existiam continuam válidos. Atende docs/enunciado.md, Entrega 2 item 5.
func TestQuoteReadsThePartnerResponse(t *testing.T) {
	var path, contentType string
	var received map[string]any

	p, closeServer := testPartner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, contentType = r.URL.Path, r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"partner":"ignored","quote_id":"q-1","premium_cents":123456,` +
			`"currency":"BRL","coverage_cents":5000000,"valid_for_seconds":300}`))
	}))
	defer closeServer()

	quote, err := NewClient(Options{}).Quote(context.Background(), p, map[string]string{"broker": "corretora-a"})
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if path != "/quotes" {
		t.Errorf("path %q, want /quotes", path)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type %q, want application/json", contentType)
	}
	if received["broker"] != "corretora-a" {
		t.Errorf("partner received broker %v, want corretora-a", received["broker"])
	}
	if quote.PremiumCents != 123456 || quote.QuoteID != "q-1" {
		t.Errorf("quote read incorrectly: %+v", quote)
	}
	if quote.Partner != "partner-flaky" {
		t.Errorf("partner %q, want partner-flaky", quote.Partner)
	}
}

func TestQuoteIdentifiesThePartnerThatFailed(t *testing.T) {
	p, closeServer := testPartner(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"partner unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer closeServer()

	_, err := NewClient(Options{}).Quote(context.Background(), p, map[string]string{})
	if err == nil {
		t.Fatal("a 503 from the partner was treated as success")
	}

	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("error %v is not a *partner.Error", err)
	}
	if failure.Partner != "partner-flaky" || failure.Status != http.StatusServiceUnavailable {
		t.Fatalf("error does not identify the failure: %+v", failure)
	}
}

func TestQuoteFailsOnUnreadableResponse(t *testing.T) {
	p, closeServer := testPartner(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer closeServer()

	if _, err := NewClient(Options{}).Quote(context.Background(), p, map[string]string{}); err == nil {
		t.Fatal("unreadable response was accepted")
	}
}

func TestQuoteHonoursCancellation(t *testing.T) {
	p, closeServer := testPartner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer closeServer()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewClient(Options{}).Quote(ctx, p, map[string]string{}); err == nil {
		t.Fatal("cancelled context did not interrupt the call")
	}
}

// Motivo: cinco 503 e a sexta não incrementa o transporte. OpenFor de uma hora impede o meio aberto no meio do teste, sem sleep. Atende docs/enunciado.md, Entrega 2 itens 1 e 5: aberto não chama a parceira, e o teste não depende de sorte.
func TestFiveFailuresOpenTheCircuitWithoutCallingAgain(t *testing.T) {
	var calls atomic.Int32
	p, closeServer := testPartner(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"partner unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer closeServer()

	client := NewClient(Options{OpenFor: time.Hour})
	for i := 0; i < 5; i++ {
		_, err := client.Quote(context.Background(), p, map[string]string{})
		if err == nil {
			t.Fatalf("failure %d was treated as success", i+1)
		}
	}
	if got := calls.Load(); got != 5 {
		t.Fatalf("transport saw %d calls after five failures, want 5", got)
	}

	_, err := client.Quote(context.Background(), p, map[string]string{})
	if got := calls.Load(); got != 5 {
		t.Fatalf("open circuit called the partner: transport count %d", got)
	}
	var failure *Error
	if !errors.As(err, &failure) || failure.Partner != "partner-flaky" {
		t.Fatalf("open circuit error %v is not a *partner.Error for partner-flaky", err)
	}
}

// Motivo: a sexta chamada conta a transição para open e não cria span de cliente. O corpo de teste traz CPF, placa e quote_id para provar que eles não viram atributo. Atende docs/enunciado.md, Entrega 2 item 4.
func TestOpenCircuitRecordsTransitionAndSkipsClientSpan(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previousMeter := otel.GetMeterProvider()
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() { otel.SetMeterProvider(previousMeter) })

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousTracer := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() { otel.SetTracerProvider(previousTracer) })

	var calls atomic.Int32
	p, closeServer := testPartner(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"partner unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer closeServer()

	client := NewClient(Options{OpenFor: time.Hour})
	request := map[string]string{"document": "12345678901", "plate": "ABC1D23", "quote_id": "q-secret"}
	ctx, span := otel.Tracer("test").Start(context.Background(), "POST /quotes")
	for i := 0; i < 5; i++ {
		if _, err := client.Quote(ctx, p, request); err == nil {
			t.Fatalf("failure %d was treated as success", i+1)
		}
	}
	clientSpans := clientSpanCount(recorder)
	if clientSpans != 5 {
		t.Fatalf("%d client spans after five failures, want 5", clientSpans)
	}

	if _, err := client.Quote(ctx, p, request); err == nil {
		t.Fatal("sixth call, with the circuit open, was treated as success")
	}
	span.End()

	if got := calls.Load(); got != 5 {
		t.Fatalf("open circuit called the partner: transport count %d", got)
	}
	if got := clientSpanCount(recorder); got != clientSpans {
		t.Fatalf("open circuit created a client span: %d client spans, want %d", got, clientSpans)
	}
	if got := transitionCount(t, reader, "partner-flaky", "closed", "open"); got != 1 {
		t.Fatalf("transition closed→open = %d, want 1", got)
	}
	assertNoPersonalData(t, reader, recorder)
}

func clientSpanCount(recorder *tracetest.SpanRecorder) int {
	n := 0
	for _, span := range recorder.Ended() {
		if span.SpanKind() == trace.SpanKindClient {
			n++
		}
	}
	return n
}

func transitionCount(t *testing.T, reader *sdkmetric.ManualReader, partner, from, to string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "quotation_breaker_transitions_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("transitions data type %T", m.Data)
			}
			for _, point := range sum.DataPoints {
				if attr(point.Attributes, "partner") == partner &&
					attr(point.Attributes, "from_state") == from &&
					attr(point.Attributes, "to_state") == to {
					total += point.Value
				}
			}
		}
	}
	return total
}

func assertNoPersonalData(t *testing.T, reader *sdkmetric.ManualReader, recorder *tracetest.SpanRecorder) {
	t.Helper()
	forbidden := []string{"12345678901", "ABC1D23", "abc1d23", "q-secret", "quote_id"}
	for _, span := range recorder.Ended() {
		for _, kv := range span.Attributes() {
			rejectPersonal(t, string(kv.Key), kv.Value.Emit(), forbidden)
		}
		for _, event := range span.Events() {
			for _, kv := range event.Attributes {
				rejectPersonal(t, string(kv.Key), kv.Value.Emit(), forbidden)
			}
		}
		if span.Status().Code == codes.Error && containsAny(span.Status().Description, forbidden) {
			t.Errorf("span status carries personal data: %s", span.Status().Description)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					for _, kv := range point.Attributes.ToSlice() {
						rejectPersonal(t, string(kv.Key), kv.Value.Emit(), forbidden)
					}
				}
			case metricdata.Gauge[int64]:
				for _, point := range data.DataPoints {
					for _, kv := range point.Attributes.ToSlice() {
						rejectPersonal(t, string(kv.Key), kv.Value.Emit(), forbidden)
					}
				}
			}
		}
	}
}

func attr(set attribute.Set, key string) string {
	value, ok := set.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return value.Emit()
}

func rejectPersonal(t *testing.T, key, value string, forbidden []string) {
	t.Helper()
	if key == "quote_id" || containsAny(key, []string{"document", "plate", "cpf"}) || containsAny(value, forbidden) {
		t.Errorf("telemetry attribute %s=%s carries personal data", key, value)
	}
}

func containsAny(value string, forbidden []string) bool {
	for _, item := range forbidden {
		if item != "" && len(value) >= len(item) && (value == item || len(item) > 0 && stringContains(value, item)) {
			return true
		}
	}
	return false
}

func stringContains(value, item string) bool {
	return len(item) > 0 && (len(value) >= len(item) && (func() bool {
		for i := 0; i+len(item) <= len(value); i++ {
			if value[i:i+len(item)] == item {
				return true
			}
		}
		return false
	})())
}
