# p95 de POST /quotes — depois

Captura com o ambiente do `make reproduce` ainda de pé. A carga é a padrão: 10 de baseline e depois 200 com 50 em voo (`docs/evidencias/depois-reproduce.txt`).

## Consulta

PromQL, janela de taxa `[1m]`:

```promql
histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{http_request_method="POST",http_route="/quotes"}[1m])))
```

A UI do Prometheus quebra a string `"/quotes"` na quebra de linha, então o gráfico abaixo usa a forma sem filtro de rota. Na série da carga as duas consultas devolvem o mesmo valor: o único histograma de servidor instrumentado é `POST /quotes`.

```promql
histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket[1m])))
```

Janela do gráfico: 10 minutos terminando em 2026-11-07 17:00:00 UTC. O relógio da UI nesta captura está em UTC (16:55 aparece como 4:55pm). A carga no relatório do loadgen vai de 16:54:40 UTC a 16:54:45 UTC.

Série da consulta filtrada, passo de 15 s (UTC):

| instante | p95 (s) |
|---|---|
| 16:55:00 | 4,206 |
| 16:55:15 | 4,205 |
| 16:55:30 | 4,219 |
| 16:57:30 | 2,425 |

O histograma do OpenTelemetry interpola entre baldes. O p95 exato do loadgen na mesma carga é 4,35 s, com p50 de 4 ms (`depois-reproduce.txt`); os dois números são desta máquina e medem instrumentos diferentes. O antes, na mesma PromQL, está em `antes-p95.md`: 9,70 s às 16:28:45 UTC, com p95 de loadgen 8,01 s.

## Gráfico

![p95 de POST /quotes no depois, janela [1m], perto de 4,2 s no fim da carga e 2,4 s no rastro seguinte.](depois-p95.png)
