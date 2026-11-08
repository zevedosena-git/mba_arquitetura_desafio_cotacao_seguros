module github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros

go 1.25.0

require (
	// Motivo: a API passa a falar com o Redis que o compose já sobe. v9.22.0 cabe em Go 1.25; a v9.23 pede Go 1.26. Atende docs/enunciado.md, Entrega 2 item 2 (Cache) e a restrição de Go 1.25.
	github.com/redis/go-redis/v9 v9.22.0
	// Motivo: a biblioteca do breaker é decisão desta entrega e precisa expor fechado, aberto e meio aberto. Atende docs/enunciado.md, Entrega 2 item 1 (Circuit breaker).
	github.com/sony/gobreaker/v2 v2.4.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.69.0
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.44.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0
	// Motivo: o código emite gauge, contador e evento no SDK que já existia, sem reescrever telemetry.go. Atende docs/enunciado.md, Entrega 2 item 4 (Instrumentação).
	go.opentelemetry.io/otel/metric v1.44.0
	go.opentelemetry.io/otel/sdk v1.44.0
	go.opentelemetry.io/otel/sdk/metric v1.44.0
	// Motivo: o curto-circuito e o hit de cache marcam o span de POST /quotes que o otelhttp já abre. Atende docs/enunciado.md, Entrega 2 item 4 (Instrumentação).
	go.opentelemetry.io/otel/trace v1.44.0
)

require (
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
