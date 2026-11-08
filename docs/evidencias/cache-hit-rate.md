# Hit rate do cache na carga

Comando da consulta, ambiente do `make reproduce` ainda de pé:

```promql
sum(rate(quotation_cache_lookups_total{result="hit"}[5m])) / sum(rate(quotation_cache_lookups_total[5m]))
```

Janela da taxa: `[5m]`. Janela do gráfico: 15 minutos terminando em 2026-11-07 17:00:00 UTC. O relógio da UI está em UTC (16:55 aparece como 4:55pm).

Legenda: a série sobe para cerca de 0,78 no fim da carga das 16:54 UTC e volta a zero quando a janela de 5 minutos esvazia; a seção 8 do SAD não usa este número.

Série, passo de 15 s (UTC):

| instante | hit rate |
|---|---|
| 16:54:45 | 0 |
| 16:55:00 | 0,778 |
| 16:56:00 | 0,776 |
| 16:57:00 | 0,777 |
| 16:58:00 | 0,751 |
| 16:59:30 | 0,750 |
| 16:59:45 | 0 |

O degrau perto de 16:57 UTC são sete cotações de placa nova (`PRB1A01` a `PRB1A04`, depois `PRB2B01` a `PRB2B03`) feitas nesta sessão para forçar a sonda do breaker; elas contam como miss. A evidência atual do breaker vem de outra sessão, em `breaker-transicoes.md`. O valor alto existe porque a carga padrão usa 5 cotações distintas. A planilha da seção 8 continua no pressuposto P1 (20% e 40%).

![Hit rate da carga, janela [5m], sobe de zero para cerca de 0,78 às 16:55 UTC e cai a zero às 16:59 UTC.](cache-hit-rate.png)
