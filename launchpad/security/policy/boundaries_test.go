package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrivateBoundaryRejectsActorHeadersWithoutWorkload(t *testing.T) {
	writes := 0
	handler := PrivateBoundary(WorkloadVerifier{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { writes++ }))
	request := httptest.NewRequest("POST", "/private", nil)
	request.Header.Set("X-Actor-Role", "operator")
	request.Header.Set("X-Actor-ID", "attacker")
	record := httptest.NewRecorder()
	handler.ServeHTTP(record, request)
	if record.Code != 401 || writes != 0 {
		t.Fatal("actor headers granted authority", record.Code, writes)
	}
	if ValidateBoundaries([]Crossing{{ID: "browser-private", Caller: "browser", Recipient: "indexer", Authority: "header", Threat: "spoofing", PublicIdentity: true}}) == nil {
		t.Fatal("public identity admitted at private service")
	}
}
