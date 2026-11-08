# Prumo Cota — proteção da cotação

A cotação deixa de ser tudo ou nada. Cada parceira tem um circuit breaker dentro de `internal/partner/client.go`: cinco falhas consecutivas, e o timeout de 2500 ms conta como falha, abrem o circuito por 5 s. Aberto não chama a parceira. A resposta segue com as propostas que chegaram, `degraded: true` e o nome de quem faltou em `missing_partners`. Só a cotação completa, uma proposta por parceira, entra no Redis por 300 s, na chave que contém a corretora. A segunda pergunta igual volta do cache, sem chamar o `Quoter`, com `served_from_cache: true` e `cache_age_seconds`. Zero propostas continua 502 `partner insurer unavailable`, com o campo `partner`. CPF, placa e `quote_id` não entram em span nem em métrica.

O enunciado do desafio está em [`docs/enunciado.md`](docs/enunciado.md). O desenho que este código segue está em [`docs/sad.md`](docs/sad.md).

## SAD e evidências

- SAD, oito seções: [`docs/sad.md`](docs/sad.md)
- Índice das sete evidências, com comando, artefato e legenda: [`docs/evidencias/README.md`](docs/evidencias/README.md)
- Carga antes, código ainda intacto: [`docs/evidencias/antes-reproduce.txt`](docs/evidencias/antes-reproduce.txt) e p95 em [`docs/evidencias/antes-p95.md`](docs/evidencias/antes-p95.md)
- Carga depois, a mesma carga padrão: [`docs/evidencias/depois-reproduce.txt`](docs/evidencias/depois-reproduce.txt) e p95 em [`docs/evidencias/depois-p95.md`](docs/evidencias/depois-p95.md)
- Suíte: [`docs/evidencias/make-test.txt`](docs/evidencias/make-test.txt)
- Curto-circuito: [`docs/evidencias/trace-curto-circuito.md`](docs/evidencias/trace-curto-circuito.md)
- Cache: [`docs/evidencias/trace-cache.md`](docs/evidencias/trace-cache.md)
- Estado do breaker no tempo, fechado → aberto → meio aberto → fechado: [`docs/evidencias/breaker-transicoes.md`](docs/evidencias/breaker-transicoes.md)
- Hit rate medido, que a seção 8 do SAD não usa na planilha: [`docs/evidencias/cache-hit-rate.md`](docs/evidencias/cache-hit-rate.md)

Nesta máquina a carga padrão passou de 120 sucessos em 200, p95 de 8,01 s, para 200 sucessos em 200, p95 de 4,35 s e p50 de 4 ms. Os dois relatórios são a medição daqui.

## Como subir e reproduzir

Pré-requisitos: Docker Compose v2, Go 1.25 e GNU Make. O `go.mod` fica em `go 1.25.0`.

```
make down
make reproduce
```

`make reproduce` sobe o compose com build e espera, e em seguida roda a carga padrão do `cmd/loadgen`: 10 requisições uma a uma e depois 200 com 50 em voo, 5 cotações distintas, tenant `corretora-a`. `make test` roda `go test ./...`. `make down` derruba o ambiente e zera Redis, Prometheus e Jaeger: o gráfico se captura com o ambiente de pé.

API em `localhost:8080` (`POST /quotes`, `GET /healthz`), Redis em `localhost:6379`, Prometheus em `localhost:9090`, Jaeger em `localhost:16686`.

## O que foi implementado e o que ficou proposta

Implementado, com os parâmetros do SAD iguais no código:

- timeout de 2500 ms e um breaker por parceira (`PARTNER_TIMEOUT_MS`, `BREAKER_CONSECUTIVE_FAILURES` 5, `BREAKER_OPEN_SECONDS` 5, `BREAKER_HALF_OPEN_SUCCESSES` 2)
- cache da cotação completa no Redis, TTL 300 s (`CACHE_TTL_SECONDS`), chave `quote:{tenant}:{coverage}:{document}:{birth_year}:{plate}:{model}:{year}:{value_cents}`
- fallback parcial: 200 com `degraded` e `missing_partners` quando sobra ao menos uma proposta
- as três métricas e os dois nomes de trace abaixo

Proposta, sem código neste repositório: auditoria persistida e a cópia desse arquivo noutro cômodo no Brasil, retry, bulkhead, rate limit, fila, autenticação e Kubernetes. Também ficam de fora, de propósito, o teto de chamadas simultâneas por parceira, a invalidação de cache antes do TTL e um breaker compartilhado entre processos. A agregação continua em série. Os perfis das parceiras no `docker-compose.yml` não mudam.

## Métricas e atributos

| Nome | Onde | Rótulos ou atributo |
|---|---|---|
| `quotation_breaker_state` | gauge, `internal/partner/client.go` | `partner`. 0 fechado, 1 meio aberto, 2 aberto |
| `quotation_breaker_transitions_total` | contador | `partner`, `from_state`, `to_state`: `closed`, `half_open`, `open` |
| `quotation_cache_lookups_total` | contador, `internal/quotation/service.go` | `result` = `hit` ou `miss` |
| `http_client_request_duration_seconds` | já emitida por `InstrumentTransport` | `server_address`. Chamada suprimida pelo aberto não gera amostra |
| `quotation.partner.short_circuited` | evento no span de `POST /quotes` | atributo `partner.name` |
| `quotation.served_from_cache` | atributo do mesmo span, no hit | `true` |

CPF, placa e `quote_id` não são atributo de span nem rótulo de métrica. `tenant_id` também fica de fora dos dois.

PromQL usadas no SAD e nestas evidências:

```promql
histogram_quantile(0.95, sum by (le, server_address) (rate(http_client_request_duration_seconds_bucket[5m])))
```

```promql
sum(rate(quotation_cache_lookups_total{result="hit"}[5m])) / sum(rate(quotation_cache_lookups_total[5m]))
```

```promql
histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket[1m])))
```

## O que eu faria diferente

Mediria o hit rate no tráfego real da corretora antes de tratar 20% e 40% como fato. A carga padrão, com cinco cotações distintas, desenha um acerto que não é o do dia útil, e a seção 8 do SAD já deixa esse gráfico fora da planilha.

Não acrescentaria retry. A chamada que o breaker segurou é exatamente o R$ 0,04 e a espera que o aberto existe para não gastar.

Gravaria a auditoria em seguida, append-only, com a cópia noutro cômodo no Brasil. Hoje a perda do site leva um arquivo que esta entrega não escreve, e a seção 8 só precifica o disco.

Deixaria o breaker na memória do processo enquanto houver uma réplica. Uma segunda réplica faria a corretora pagar de novo uma parceira que o processo anterior já tinha aberto, e aí o estado precisaria sair do processo. Esta entrega não sobe a segunda réplica.
