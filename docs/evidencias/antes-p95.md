# p95 de POST /quotes — antes

Captura com o ambiente do `make reproduce` ainda de pé, código Go intacto. A carga é a padrão: 10 de baseline e depois 200 com 50 em voo (`docs/evidencias/antes-reproduce.txt`).

## Consulta

PromQL, janela de taxa `[1m]`:

```promql
histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{http_request_method="POST",http_route="/quotes"}[1m])))
```

A UI do Prometheus quebra a string `"/quotes"` na quebra de linha, então o gráfico abaixo usa a forma sem filtro de rota. Nesta execução as duas consultas devolveram o mesmo valor: o único histograma de servidor instrumentado é `POST /quotes` (`GET /healthz` não entra em `InstrumentHandler`).

```promql
histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket[1m])))
```

Janela do gráfico: 5 minutos terminando em 2026-11-07 16:32:00 UTC. O relógio da UI nesta captura está em UTC (16:28 aparece como 4:28pm). A carga no relatório do loadgen vai de 16:28:03 UTC a 16:28:28 UTC.

Instante 2026-11-07 16:28:45 UTC, as duas consultas: **9,70 s**.

Série da consulta filtrada, passo de 15 s (UTC):

| instante | p95 (s) |
|---|---|
| 16:28:15 | 2,425 |
| 16:28:30 | 9,665 |
| 16:28:45 | 9,702 |
| 16:29:00 | 9,698 |
| 16:29:15 | 9,824 |
| 16:29:30 | 9,875 |

O histograma do OpenTelemetry interpola entre os baldes de 7,5 s e 10 s. O p95 exato do loadgen na mesma carga é 8,01 s (`antes-reproduce.txt`); os dois números são desta máquina e medem instrumentos diferentes.

## Gráfico

![p95 de POST /quotes no antes, janela [1m], sobe de cerca de 2,4 s no baseline para cerca de 9,7 s no fim da carga.](antes-p95.png)
