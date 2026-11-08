package quotation

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/partner"
	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/platform"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Quoter interface {
	Quote(ctx context.Context, p platform.Partner, request any) (partner.Quote, error)
}

type Service struct {
	partners []platform.Partner
	quoter   Quoter
	cache    Cache
	lookups  metric.Int64Counter
}

func NewService(partners []platform.Partner, quoter Quoter, cache Cache) *Service {
	meter := otel.Meter("github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/quotation")
	// Motivo: hit e miss com rótulo result, para o hit rate em PromQL. Sem CPF, placa ou quote_id. Atende docs/enunciado.md, Entrega 2 item 4 (Instrumentação).
	lookups, err := meter.Int64Counter("quotation_cache_lookups_total")
	if err != nil {
		panic(fmt.Errorf("quotation_cache_lookups_total: %w", err))
	}
	return &Service{partners: partners, quoter: quoter, cache: cache, lookups: lookups}
}

func (s *Service) Quote(ctx context.Context, tenant string, request Request) (Response, error) {
	start := time.Now()
	// Motivo: cache nil nos testes antigos segue sempre em miss, para a agregação em série continuar verificável. Atende docs/enunciado.md, Entrega 2 item 5 (não quebrar o que já passa) e o bônus de paralelização, que não foi feito.
	if s.cache != nil {
		// Motivo: a segunda cotação igual volta do cache, marcada, com a idade, e sem chamar a parceira. Atende docs/enunciado.md, Entrega 2 itens 2 (Cache) e 3 (cotação anterior servida do cache).
		if cached, age, ok := s.cache.Get(ctx, tenant, request); ok {
			s.recordLookup(ctx, "hit")
			cached.TenantID = tenant
			cached.Degraded = false
			if cached.MissingPartners == nil {
				cached.MissingPartners = []string{}
			}
			cached.ServedFromCache = true
			cached.CacheAgeSeconds = &age
			cached.ElapsedMs = time.Since(start).Milliseconds()
			// Motivo: o trace da cotação servida de cache não pode parecer uma cotação que consultou as três. Atende docs/enunciado.md, Entrega 2 item 4.
			trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("quotation.served_from_cache", true))
			return cached, nil
		}
		s.recordLookup(ctx, "miss")
	}

	forPartner := partnerRequest{Broker: tenant, Request: request}
	quotes := make([]partner.Quote, 0, len(s.partners))
	missing := make([]string, 0)
	// Motivo: uma parceira falha e as propostas que já chegaram são entregues, com o nome de quem faltou. O laço segue em série. Atende docs/enunciado.md, Entrega 2 item 3 (Fallback: resposta parcial).
	for _, p := range s.partners {
		quote, err := s.quoter.Quote(ctx, p, forPartner)
		if err != nil {
			missing = append(missing, p.Name)
			continue
		}
		quotes = append(quotes, quote)
	}

	sort.Slice(quotes, func(i, j int) bool {
		return quotes[i].PremiumCents < quotes[j].PremiumCents
	})

	// Motivo: sem nenhuma proposta a resposta continua 502 com o campo partner, que o loadgen lê. Atende docs/enunciado.md, Entrega 2 item 3: recusa explícita quando não há o que entregar, e prêmio inventado continua proibido.
	if len(quotes) == 0 {
		name := ""
		if len(missing) > 0 {
			name = missing[0]
		}
		return Response{}, &partner.Error{Partner: name, Reason: "partner insurer unavailable"}
	}

	response := Response{
		TenantID:        tenant,
		Quotes:          quotes,
		ElapsedMs:       time.Since(start).Milliseconds(),
		Degraded:        len(missing) > 0,
		MissingPartners: missing,
		ServedFromCache: false,
	}
	// Motivo: só a cotação completa entra no cache. Com o breaker aberto a entrada vencida não é regravada, e erro de SET não derruba a resposta. Atende docs/enunciado.md, Entrega 2 item 2: o que acontece com a entrada quando o breaker abre.
	if len(missing) == 0 && s.cache != nil {
		_ = s.cache.Set(ctx, tenant, request, response)
	}
	return response, nil
}

func (s *Service) recordLookup(ctx context.Context, result string) {
	s.lookups.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
