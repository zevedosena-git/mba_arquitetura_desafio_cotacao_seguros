package quotation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/partner"
)

// Motivo: outra corretora não lê a entrada, e placa em minúsculas acerta a mesma chave. Atende docs/enunciado.md, Entrega 2 item 2: chave sem a corretora reprova.
func TestCacheKeyIsolatesTheBrokerAndNormalizesThePlate(t *testing.T) {
	request := validRequest()
	lower := request
	lower.Vehicle.Plate = "abc1d23"

	if Key("corretora-a", request) != Key("corretora-a", lower) {
		t.Fatal("lowercase plate produced a different cache key")
	}
	if Key("corretora-a", request) == Key("corretora-b", request) {
		t.Fatal("two brokers share a cache key")
	}
	if !strings.Contains(Key("corretora-a", request), "corretora-a") {
		t.Fatal("cache key does not contain the broker")
	}

	other := request
	other.Vehicle.Plate = "XYZ9Z99"
	if Key("corretora-a", request) == Key("corretora-a", other) {
		t.Fatal("two plates share a cache key")
	}
}

// Motivo: entrada com mais de 300 s não volta, sem esperar o relógio real. Atende docs/enunciado.md, Entrega 2 itens 2 e 5 (TTL, teste sem sleep).
func TestMemoryCacheDoesNotCrossBrokersOrServeExpiredEntries(t *testing.T) {
	now := time.Date(2026, 11, 7, 13, 0, 0, 0, time.UTC)
	cache := NewMemoryCache(300*time.Second, func() time.Time { return now })
	stored := Response{TenantID: "corretora-a", Quotes: partnerQuote(t), Degraded: false, MissingPartners: []string{}, ServedFromCache: false}
	request := validRequest()
	if err := cache.Set(context.Background(), "corretora-a", request, stored); err != nil {
		t.Fatalf("Set: %v", err)
	}

	lower := request
	lower.Vehicle.Plate = "  abc1d23 "
	if _, _, ok := cache.Get(context.Background(), "corretora-a", lower); !ok {
		t.Fatal("lowercase plate missed an entry stored in uppercase")
	}
	if _, _, ok := cache.Get(context.Background(), "corretora-b", request); ok {
		t.Fatal("corretora-b read the entry stored for corretora-a")
	}

	now = now.Add(301 * time.Second)
	if _, _, ok := cache.Get(context.Background(), "corretora-a", request); ok {
		t.Fatal("entry older than 300s was served")
	}
}

func partnerQuote(t *testing.T) []partner.Quote {
	t.Helper()
	return []partner.Quote{{Partner: "partner-slow", PremiumCents: 1}}
}
