# Trace de curto-circuito

Comando, com o ambiente do `make reproduce` ainda de pé:

```
GET http://localhost:16686/api/traces?service=quotation-api&operation=POST%20/quotes&limit=80&lookback=1h
```

O trace gravado em `trace-curto-circuito.json` é `f6f776270eee63babf94062b9518490d`.

Legenda: `POST /quotes` carrega o evento `quotation.partner.short_circuited` com `partner.name=partner-flaky` e só dois spans `HTTP POST`; a parceira aberta não foi chamada, e CPF, placa e `quote_id` não aparecem no trace.
