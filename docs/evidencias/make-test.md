# make test

Legenda: a suíte inteira passa sem cache, os testes que já vinham e os desta entrega, e o `make smoke` continua verde.

O `make` não está instalado nesta máquina; cada alvo é uma linha de `go` no `Makefile`, e a linha literal foi executada:

```
go clean -testcache
go test ./...                                        # make test
go test -count=1 -v ./...                            # a mesma suíte, listando cada teste
go test ./cmd/partner-mock -run Feasibility -v       # make smoke
```

Saída completa em `make-test.txt`. As três invocações terminaram com código 0, sem `--- FAIL` e sem `--- SKIP`. Os testes desta entrega aparecem pelo nome: `TestFiveFailuresOpenTheCircuitWithoutCallingAgain`, `TestOpenCircuitRecordsTransitionAndSkipsClientSpan`, `TestSecondIdenticalCallIsServedFromCache`, `TestPartialQuoteIsNotCached`, `TestCacheKeyIsolatesTheBrokerAndNormalizesThePlate`, `TestMemoryCacheDoesNotCrossBrokersOrServeExpiredEntries`, `TestQuotesResponds502WhenNoPartnerAnswers`, `TestProtectionDefaultsMatchTheDesign` e `TestProtectionReadsExplicitValues`.
