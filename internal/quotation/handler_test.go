package quotation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/platform"
)

const validBody = `{
  "driver": {"document": "12345678901", "birth_year": 1988},
  "vehicle": {"plate": "abc1d23", "model": "Gol 1.0", "year": 2020, "value_cents": 8500000},
  "coverage": "comprehensive"
}`

func testAPI(quoter Quoter) http.Handler {
	// Motivo: cache nil preserva os testes de contrato que já passavam. Atende docs/enunciado.md, Entrega 2 item 5.
	return NewAPI(NewService(threePartners, quoter, nil), []string{"corretora-a", "corretora-b"}).Routes()
}

func postQuotes(h http.Handler, tenant, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/quotes", strings.NewReader(body))
	if tenant != "" {
		req.Header.Set(TenantHeader, tenant)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	return response
}

func TestQuotesReturnsAggregatedQuotes(t *testing.T) {
	response := postQuotes(testAPI(&fakeQuoter{premiums: defaultPremiums()}), "corretora-a", validBody)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d, expected 200: %s", response.Code, response.Body)
	}

	var body Response
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unreadable response: %v", err)
	}
	if len(body.Quotes) != 3 {
		t.Fatalf("%d quotes, expected 3", len(body.Quotes))
	}
	if body.TenantID != "corretora-a" {
		t.Errorf("tenant_id %q, expected corretora-a", body.TenantID)
	}
}

func TestQuotesRejectsRequestWithoutTenant(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums()}
	response := postQuotes(testAPI(quoter), "", validBody)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status %d, expected 400", response.Code)
	}
	if len(quoter.calls) != 0 {
		t.Fatalf("the partners were called (%v) even without a tenant", quoter.calls)
	}
}

func TestQuotesRejectsUnknownBroker(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums()}
	response := postQuotes(testAPI(quoter), "corretora-pirata", validBody)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status %d, expected 403", response.Code)
	}
	if len(quoter.calls) != 0 {
		t.Fatalf("the partners were called (%v) for a broker that is not enabled", quoter.calls)
	}
}

func TestQuotesRejectsInvalidBody(t *testing.T) {
	cases := map[string]string{
		"broken json":      `{"driver":`,
		"missing document": `{"driver":{"birth_year":1988},"vehicle":{"plate":"ABC1D23","year":2020,"value_cents":100}}`,
		"missing plate":    `{"driver":{"document":"1","birth_year":1988},"vehicle":{"year":2020,"value_cents":100}}`,
		"zero value":       `{"driver":{"document":"1","birth_year":1988},"vehicle":{"plate":"A","year":2020,"value_cents":0}}`,
		"invalid coverage": `{"driver":{"document":"1","birth_year":1988},"vehicle":{"plate":"A","year":2020,"value_cents":1},"coverage":"vip"}`,
		"unknown field":    `{"driver":{"document":"1","birth_year":1988},"vehicle":{"plate":"A","year":2020,"value_cents":1},"discount":true}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response := postQuotes(testAPI(&fakeQuoter{premiums: defaultPremiums()}), "corretora-a", body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status %d, expected 400: %s", response.Code, response.Body)
			}
		})
	}
}

// Motivo: uma parceira fora deixa de ser 502 da requisição inteira e passa a ser 200 com degraded e missing_partners. Atende docs/enunciado.md, Entrega 2 item 3. O nome do teste permanece o que já existia.
func TestQuotesResponds502WithTheNameOfThePartnerThatWentDown(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums(), failOn: "partner-flaky"}
	response := postQuotes(testAPI(quoter), "corretora-a", validBody)

	if response.Code != http.StatusOK {
		t.Fatalf("status %d, expected 200 partial: %s", response.Code, response.Body)
	}
	if response.Header().Get(TenantHeader) != "corretora-a" {
		t.Errorf("X-Tenant-Id %q, expected corretora-a", response.Header().Get(TenantHeader))
	}
	if strings.Contains(response.Body.String(), "cache_age_seconds") {
		t.Fatalf("partial body includes cache_age_seconds: %s", response.Body)
	}

	var body Response
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unreadable response: %v", err)
	}
	if !body.Degraded || body.ServedFromCache || len(body.Quotes) != 2 {
		t.Fatalf("partial contract: %+v", body)
	}
	if len(body.MissingPartners) != 1 || body.MissingPartners[0] != "partner-flaky" {
		t.Errorf("missing_partners %v, expected partner-flaky", body.MissingPartners)
	}
}

// Motivo: zero propostas continua 502 partner insurer unavailable com o campo partner, que o loadgen lê. Atende docs/enunciado.md, Entrega 2 item 3.
func TestQuotesResponds502WhenNoPartnerAnswers(t *testing.T) {
	quoter := &fakeQuoter{premiums: defaultPremiums(), failAll: true}
	response := postQuotes(testAPI(quoter), "corretora-a", validBody)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status %d, expected 502: %s", response.Code, response.Body)
	}

	var body platform.ErrorBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unreadable error: %v", err)
	}
	if body.Message != "partner insurer unavailable" || body.Partner != "partner-slow" {
		t.Fatalf("body %+v, expected partner insurer unavailable from partner-slow", body)
	}
}

func TestHealthz(t *testing.T) {
	response := httptest.NewRecorder()
	testAPI(&fakeQuoter{premiums: defaultPremiums()}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status %d, expected 200", response.Code)
	}
}
