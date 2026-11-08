# Trace servido de cache

Comando, o mesmo GET do Jaeger da carga ainda de pé:

```
GET http://localhost:16686/api/traces?service=quotation-api&operation=POST%20/quotes&limit=80&lookback=1h
```

O trace gravado em `trace-cache.json` é `6f7a79e20bf0739cf96745290941e9a3`.

Legenda: o único span é `POST /quotes`, com `quotation.served_from_cache=true` e duração de 245 µs; não há span de cliente, e CPF, placa e `quote_id` não aparecem no trace.
