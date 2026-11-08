package partner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/platform"
	"github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Quote struct {
	Partner         string `json:"partner"`
	QuoteID         string `json:"quote_id"`
	PremiumCents    int64  `json:"premium_cents"`
	Currency        string `json:"currency"`
	CoverageCents   int64  `json:"coverage_cents"`
	ValidForSeconds int64  `json:"valid_for_seconds"`
}

// Options carrega o timeout e os limiares do breaker.
// Motivo: o client nascia sem Timeout, então a partner-degrading nunca virava falha. Atende docs/enunciado.md, Entrega 2 item 1 (Circuit breaker): o timeout é obrigatório e o estouro conta como falha.
type Options struct {
	Timeout             time.Duration
	ConsecutiveFailures int
	OpenFor             time.Duration
	HalfOpenSuccesses   int
}

// Motivo: teste e processo sem env usam os mesmos números do SAD (2500 ms, 5 falhas, 5 s, 2 sucessos). Atende docs/enunciado.md, Entrega 2 item 1: os parâmetros vêm do SAD, não de chute no código.
func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = 2500 * time.Millisecond
	}
	if o.ConsecutiveFailures <= 0 {
		o.ConsecutiveFailures = 5
	}
	if o.OpenFor <= 0 {
		o.OpenFor = 5 * time.Second
	}
	if o.HalfOpenSuccesses <= 0 {
		o.HalfOpenSuccesses = 2
	}
	return o
}

type Client struct {
	http     *http.Client
	opts     Options
	state    metric.Int64Gauge
	changes  metric.Int64Counter
	mu       sync.Mutex
	breakers map[string]*gobreaker.CircuitBreaker[Quote]
}

func NewClient(opts Options) *Client {
	opts = opts.withDefaults()
	meter := otel.Meter("github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/partner")
	// Motivo: estado observável e contador de transições, no SDK que já existia. telemetry.go não foi reescrito. Atende docs/enunciado.md, Entrega 2 item 4.
	state, err := meter.Int64Gauge("quotation_breaker_state")
	if err != nil {
		panic(fmt.Errorf("quotation_breaker_state: %w", err))
	}
	changes, err := meter.Int64Counter("quotation_breaker_transitions_total")
	if err != nil {
		panic(fmt.Errorf("quotation_breaker_transitions_total: %w", err))
	}
	return &Client{
		// Motivo: o teto de espera faz a lentidão da partner-degrading contar como falha, e a latência de quem é chamado continua em http_client_request_duration_seconds. Atende docs/enunciado.md, Entrega 2 itens 1 e 4.
		http:     &http.Client{Timeout: opts.Timeout, Transport: platform.InstrumentTransport(http.DefaultTransport)},
		opts:     opts,
		state:    state,
		changes:  changes,
		breakers: map[string]*gobreaker.CircuitBreaker[Quote]{},
	}
}

const responseLimit = 1 << 20

func (c *Client) Quote(ctx context.Context, p platform.Partner, request any) (Quote, error) {
	// Motivo: um breaker por parceira. Aberto não chama a parceira, então a consulta de R$ 0,04 e a espera não acontecem. Atende docs/enunciado.md, Entrega 2 item 1.
	quote, err := c.breaker(p.Name).Execute(func() (Quote, error) {
		return c.call(ctx, p, request)
	})
	// Motivo: a chamada que não saiu marca o trace, senão a cotação parece só rápida. O atributo é o nome da parceira; CPF, placa e quote_id ficam de fora. Atende docs/enunciado.md, Entrega 2 item 4.
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		trace.SpanFromContext(ctx).AddEvent("quotation.partner.short_circuited", trace.WithAttributes(
			attribute.String("partner.name", p.Name),
		))
		return Quote{}, &Error{Partner: p.Name, Reason: err.Error()}
	}
	return quote, err
}

func (c *Client) breaker(name string) *gobreaker.CircuitBreaker[Quote] {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.breakers[name]; ok {
		return existing
	}

	// Motivo: a biblioteca abre com mais de 5 falhas; o requisito pede abrir na 5ª. Meio aberto deixa passar as sondas e uma falha reabre. Atende docs/enunciado.md, Entrega 2 item 1.
	created := gobreaker.NewCircuitBreaker[Quote](gobreaker.Settings{
		Name:        name,
		MaxRequests: uint32(c.opts.HalfOpenSuccesses),
		Interval:    0,
		Timeout:     c.opts.OpenFor,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(c.opts.ConsecutiveFailures)
		},
		// Motivo: estado atual e transição com origem e destino, por parceira, para desenhar fechado → aberto → meio aberto → fechado. Atende docs/enunciado.md, Entrega 2 item 4.
		OnStateChange: func(partnerName string, from, to gobreaker.State) {
			attrs := metric.WithAttributes(
				attribute.String("partner", partnerName),
				attribute.String("from_state", stateName(from)),
				attribute.String("to_state", stateName(to)),
			)
			c.changes.Add(context.Background(), 1, attrs)
			c.state.Record(context.Background(), stateValue(to), metric.WithAttributes(
				attribute.String("partner", partnerName),
			))
		},
	})
	c.state.Record(context.Background(), 0, metric.WithAttributes(attribute.String("partner", name)))
	c.breakers[name] = created
	return created
}

// Motivo: o rótulo da biblioteca é "half-open"; a série usa half_open, o nome combinado no SAD. Atende docs/enunciado.md, Entrega 2 item 4: a métrica citada tem de ser a que o código emite.
func stateName(s gobreaker.State) string {
	switch s {
	case gobreaker.StateHalfOpen:
		return "half_open"
	case gobreaker.StateOpen:
		return "open"
	default:
		return "closed"
	}
}

func stateValue(s gobreaker.State) int64 {
	switch s {
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	default:
		return 0
	}
}

func (c *Client) call(ctx context.Context, p platform.Partner, request any) (Quote, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: fmt.Sprintf("invalid request: %v", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/quotes", bytes.NewReader(body))
	if err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(req)
	// Motivo: estouro do Timeout volta como erro e o breaker conta falha. Sem isso a partner-degrading não abre. Atende docs/enunciado.md, Entrega 2 item 1.
	if err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: err.Error()}
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return Quote{}, &Error{
			Partner: p.Name,
			Status:  response.StatusCode,
			Reason:  fmt.Sprintf("partner replied %d", response.StatusCode),
		}
	}

	var quote Quote
	if err := json.NewDecoder(io.LimitReader(response.Body, responseLimit)).Decode(&quote); err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: fmt.Sprintf("unreadable response: %v", err)}
	}

	quote.Partner = p.Name
	return quote, nil
}

type Error struct {
	Partner string
	Status  int
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("partner %s: %s", e.Partner, e.Reason)
}
