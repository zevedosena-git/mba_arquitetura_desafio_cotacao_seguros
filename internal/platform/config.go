package platform

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port      string
	Partners  []Partner
	Tenants   []string
	Telemetry Telemetry
	// Motivo: timeout, limiares do breaker, TTL e endereço do Redis saem do ambiente, com o mesmo número do SAD. Atende docs/enunciado.md, Entrega 2 itens 1 e 2, e o mapa de internal/platform/config.go (novos parâmetros).
	PartnerTimeout             time.Duration
	BreakerConsecutiveFailures int
	BreakerOpen                time.Duration
	BreakerHalfOpenSuccesses   int
	CacheTTL                   time.Duration
	RedisAddr                  string
}

type Partner struct {
	Name    string
	BaseURL string
}

type Telemetry struct {
	ServiceName string
	Endpoint    string
	Enabled     bool
}

const defaultEndpoints = "partner-slow=http://localhost:9001," +
	"partner-flaky=http://localhost:9002," +
	"partner-degrading=http://localhost:9003"

const defaultTenants = "corretora-a,corretora-b"

const defaultCollector = "http://localhost:4317"

const defaultServiceName = "quotation-api"

func LoadConfig(env func(string) string) (Config, error) {
	cfg := Config{Port: text(env, "PORT", "8080")}

	var err error
	if cfg.Partners, err = parsePartners(text(env, "PARTNER_ENDPOINTS", defaultEndpoints)); err != nil {
		return Config{}, err
	}
	if cfg.Tenants, err = parseTenants(text(env, "TENANTS", defaultTenants)); err != nil {
		return Config{}, err
	}
	if cfg.Telemetry, err = parseTelemetry(env); err != nil {
		return Config{}, err
	}
	// Motivo: default vazio usa 2500 ms, 5 falhas, 5 s aberto, 2 sucessos e TTL de 300 s, abaixo do teto comercial de 24 h. Atende docs/enunciado.md, Entrega 2 itens 1 e 2.
	if cfg.PartnerTimeout, err = durationMillis(env, "PARTNER_TIMEOUT_MS", 2500); err != nil {
		return Config{}, err
	}
	if cfg.BreakerConsecutiveFailures, err = positiveInt(env, "BREAKER_CONSECUTIVE_FAILURES", 5); err != nil {
		return Config{}, err
	}
	openSeconds, err := positiveInt(env, "BREAKER_OPEN_SECONDS", 5)
	if err != nil {
		return Config{}, err
	}
	cfg.BreakerOpen = time.Duration(openSeconds) * time.Second
	if cfg.BreakerHalfOpenSuccesses, err = positiveInt(env, "BREAKER_HALF_OPEN_SUCCESSES", 2); err != nil {
		return Config{}, err
	}
	ttlSeconds, err := positiveInt(env, "CACHE_TTL_SECONDS", 300)
	if err != nil {
		return Config{}, err
	}
	cfg.CacheTTL = time.Duration(ttlSeconds) * time.Second
	cfg.RedisAddr = text(env, "REDIS_ADDR", "localhost:6379")
	return cfg, nil
}

func durationMillis(env func(string) string, key string, fallback int) (time.Duration, error) {
	millis, err := positiveInt(env, key, fallback)
	if err != nil {
		return 0, err
	}
	return time.Duration(millis) * time.Millisecond, nil
}

func positiveInt(env func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(env(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s: %q is not a positive integer", key, raw)
	}
	return n, nil
}

func parseTelemetry(env func(string) string) (Telemetry, error) {
	telemetry := Telemetry{
		ServiceName: text(env, "OTEL_SERVICE_NAME", defaultServiceName),
		Endpoint:    strings.TrimSpace(text(env, "OTEL_EXPORTER_OTLP_ENDPOINT", defaultCollector)),
	}

	switch disabled := strings.ToLower(strings.TrimSpace(env("OTEL_SDK_DISABLED"))); disabled {
	case "", "false":
		telemetry.Enabled = true
	case "true":
		return Telemetry{Enabled: false}, nil
	default:
		return Telemetry{}, fmt.Errorf("OTEL_SDK_DISABLED: %q is neither true nor false", disabled)
	}

	if u, err := url.Parse(telemetry.Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		return Telemetry{}, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT: %q is not an absolute URL", telemetry.Endpoint)
	}
	return telemetry, nil
}

func parsePartners(raw string) ([]Partner, error) {
	var partners []Partner
	seen := map[string]bool{}

	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		name, endpoint, found := strings.Cut(item, "=")
		name, endpoint = strings.TrimSpace(name), strings.TrimSpace(endpoint)
		if !found || name == "" || endpoint == "" {
			return nil, fmt.Errorf("PARTNER_ENDPOINTS: %q is not in the name=url format", item)
		}

		if u, err := url.Parse(endpoint); err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("PARTNER_ENDPOINTS: %q is not an absolute URL", endpoint)
		}
		if seen[name] {
			return nil, fmt.Errorf("PARTNER_ENDPOINTS: partner %q is repeated", name)
		}

		seen[name] = true
		partners = append(partners, Partner{Name: name, BaseURL: strings.TrimRight(endpoint, "/")})
	}

	if len(partners) == 0 {
		return nil, fmt.Errorf("PARTNER_ENDPOINTS: at least one partner is required")
	}
	return partners, nil
}

func parseTenants(raw string) ([]string, error) {
	var tenants []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tenants = append(tenants, t)
		}
	}
	if len(tenants) == 0 {
		return nil, fmt.Errorf("TENANTS: at least one broker is required")
	}
	return tenants, nil
}

func Environment() func(string) string { return os.Getenv }

func text(env func(string) string, key, fallback string) string {
	if v := env(key); v != "" {
		return v
	}
	return fallback
}
