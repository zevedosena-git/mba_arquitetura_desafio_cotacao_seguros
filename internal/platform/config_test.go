package platform

import (
	"reflect"
	"testing"
	"time"
)

func env(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestDefaultConfigPointsAtTheComposeEnvironment(t *testing.T) {
	cfg, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("port %q, want 8080", cfg.Port)
	}
	want := []Partner{
		{Name: "partner-slow", BaseURL: "http://localhost:9001"},
		{Name: "partner-flaky", BaseURL: "http://localhost:9002"},
		{Name: "partner-degrading", BaseURL: "http://localhost:9003"},
	}
	if !reflect.DeepEqual(cfg.Partners, want) {
		t.Errorf("partners %+v, want %+v", cfg.Partners, want)
	}
	if !reflect.DeepEqual(cfg.Tenants, []string{"corretora-a", "corretora-b"}) {
		t.Errorf("brokers %v, want corretora-a and corretora-b", cfg.Tenants)
	}
}

func TestConfigReadsPartnersAndTenantsFromTheEnvironment(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{
		"PORT":              "9090",
		"PARTNER_ENDPOINTS": " a=http://a:8080/ , b=http://b:8080 ",
		"TENANTS":           " corretora-x , corretora-y ",
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	want := []Partner{{Name: "a", BaseURL: "http://a:8080"}, {Name: "b", BaseURL: "http://b:8080"}}
	if !reflect.DeepEqual(cfg.Partners, want) {
		t.Errorf("partners %+v, want %+v", cfg.Partners, want)
	}
	if !reflect.DeepEqual(cfg.Tenants, []string{"corretora-x", "corretora-y"}) {
		t.Errorf("unexpected brokers %v", cfg.Tenants)
	}
	if cfg.Port != "9090" {
		t.Errorf("port %q, want 9090", cfg.Port)
	}
}

// Motivo: o default vazio tem de ser o número do SAD, senão o limiar do código diverge do documento. Atende docs/enunciado.md, Entrega 2: subordinação ao SAD, itens 1 e 2.
func TestProtectionDefaultsMatchTheDesign(t *testing.T) {
	cfg, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.PartnerTimeout != 2500*time.Millisecond {
		t.Errorf("PARTNER_TIMEOUT_MS default %s, want 2500ms", cfg.PartnerTimeout)
	}
	if cfg.BreakerConsecutiveFailures != 5 {
		t.Errorf("BREAKER_CONSECUTIVE_FAILURES default %d, want 5", cfg.BreakerConsecutiveFailures)
	}
	if cfg.BreakerOpen != 5*time.Second {
		t.Errorf("BREAKER_OPEN_SECONDS default %s, want 5s", cfg.BreakerOpen)
	}
	if cfg.BreakerHalfOpenSuccesses != 2 {
		t.Errorf("BREAKER_HALF_OPEN_SUCCESSES default %d, want 2", cfg.BreakerHalfOpenSuccesses)
	}
	if cfg.CacheTTL != 300*time.Second {
		t.Errorf("CACHE_TTL_SECONDS default %s, want 300s", cfg.CacheTTL)
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("REDIS_ADDR default %q, want localhost:6379", cfg.RedisAddr)
	}
}

// Motivo: um valor explícito de ambiente substitui o default, que é como o TTL chega a produção e como se reverte. Atende docs/enunciado.md, Entrega 2 itens 1 e 2, e o mapa de config.go.
func TestProtectionReadsExplicitValues(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{
		"PARTNER_TIMEOUT_MS":           "1800",
		"BREAKER_CONSECUTIVE_FAILURES": "3",
		"BREAKER_OPEN_SECONDS":         "9",
		"BREAKER_HALF_OPEN_SUCCESSES":  "4",
		"CACHE_TTL_SECONDS":            "120",
		"REDIS_ADDR":                   "redis:6379",
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.PartnerTimeout != 1800*time.Millisecond {
		t.Errorf("timeout %s, want 1800ms", cfg.PartnerTimeout)
	}
	if cfg.BreakerConsecutiveFailures != 3 {
		t.Errorf("consecutive failures %d, want 3", cfg.BreakerConsecutiveFailures)
	}
	if cfg.BreakerOpen != 9*time.Second {
		t.Errorf("open %s, want 9s", cfg.BreakerOpen)
	}
	if cfg.BreakerHalfOpenSuccesses != 4 {
		t.Errorf("half-open successes %d, want 4", cfg.BreakerHalfOpenSuccesses)
	}
	if cfg.CacheTTL != 120*time.Second {
		t.Errorf("ttl %s, want 120s", cfg.CacheTTL)
	}
	if cfg.RedisAddr != "redis:6379" {
		t.Errorf("redis %q, want redis:6379", cfg.RedisAddr)
	}
}

func TestTelemetryIsOnByDefault(t *testing.T) {
	cfg, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	want := Telemetry{ServiceName: "quotation-api", Endpoint: "http://localhost:4317", Enabled: true}
	if cfg.Telemetry != want {
		t.Errorf("telemetry %+v, want %+v", cfg.Telemetry, want)
	}
}

func TestTelemetryReadsTheStandardOTelVariables(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{
		"OTEL_SERVICE_NAME":           "quotation-api-poc",
		"OTEL_EXPORTER_OTLP_ENDPOINT": " http://otel-collector:4317 ",
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	want := Telemetry{ServiceName: "quotation-api-poc", Endpoint: "http://otel-collector:4317", Enabled: true}
	if cfg.Telemetry != want {
		t.Errorf("telemetry %+v, want %+v", cfg.Telemetry, want)
	}
}

func TestTelemetryCanBeTurnedOff(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{"OTEL_SDK_DISABLED": "TRUE"}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Telemetry.Enabled {
		t.Error("OTEL_SDK_DISABLED=TRUE did not turn the SDK off")
	}
}

func TestTelemetryOffSkipsEndpointValidation(t *testing.T) {
	if _, err := LoadConfig(env(map[string]string{
		"OTEL_SDK_DISABLED":           "true",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "not-a-url",
	})); err != nil {
		t.Fatalf("LoadConfig with the SDK off: %v", err)
	}
}

func TestInvalidConfigFails(t *testing.T) {
	cases := map[string]map[string]string{
		"partner without url":                {"PARTNER_ENDPOINTS": "partner-slow"},
		"relative url":                       {"PARTNER_ENDPOINTS": "partner-slow=/quotes"},
		"url without host":                   {"PARTNER_ENDPOINTS": "partner-slow=http://"},
		"partner without name":               {"PARTNER_ENDPOINTS": "=http://a:8080"},
		"repeated partner":                   {"PARTNER_ENDPOINTS": "a=http://a:8080,a=http://b:8080"},
		"empty partner list":                 {"PARTNER_ENDPOINTS": " , "},
		"empty tenant list":                  {"TENANTS": " , "},
		"collector without scheme":           {"OTEL_EXPORTER_OTLP_ENDPOINT": "otel-collector:4317"},
		"collector without host":             {"OTEL_EXPORTER_OTLP_ENDPOINT": "http://"},
		"OTEL_SDK_DISABLED is not a boolean": {"OTEL_SDK_DISABLED": "maybe"},
	}

	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(env(vars)); err == nil {
				t.Fatalf("invalid configuration (%v) was accepted", vars)
			}
		})
	}
}
