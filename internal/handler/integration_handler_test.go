package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/integration"
	"go-fhir-phase1/internal/middleware"
)

type intakeStore struct {
	calls     int
	duplicate bool
	failure   error
	scope     integration.Scope
}

func (s *intakeStore) Receive(_ context.Context, scope integration.Scope, _ integration.Submission) (integration.Receipt, bool, error) {
	s.calls++
	s.scope = scope
	return integration.Receipt{ID: "msg-test", State: "needs_reconciliation", Reason: "patient-unmatched"}, s.duplicate, s.failure
}
func (s *intakeStore) Get(_ context.Context, scope integration.Scope, id string) (integration.Receipt, error) {
	s.scope = scope
	if id != "msg-test" {
		return integration.Receipt{}, internal.ErrNotFound
	}
	return integration.Receipt{ID: id, State: "needs_reconciliation"}, s.failure
}

func TestIntegrationIntakeHTTP(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/integration/synthetic-result.json")
	if err != nil {
		t.Fatal(err)
	}
	store := &intakeStore{}
	scope := integration.Scope{TenantID: "synthetic-demo", SourceID: "synthetic-lis"}
	h := NewIntegrationHandler(integration.NewService(store), scope)
	protected := middleware.AuthMiddleware("test-token", h.HandleResults)
	request := func(method, path, token, media string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Content-Type", media)
		w := httptest.NewRecorder()
		protected(w, r)
		return w
	}
	if w := request("POST", "/api/v1/integration/results", "", "application/json", raw); w.Code != 401 || store.calls != 0 {
		t.Fatal("unauthenticated receipt stored")
	}
	w := request("POST", "/api/v1/integration/results", "test-token", "application/json", raw)
	if w.Code != 202 || w.Header().Get("Location") != "/api/v1/integration/results/msg-test" || store.scope != scope {
		t.Fatalf("intake: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "00001") || strings.Contains(w.Body.String(), "SYN-ORDER") {
		t.Fatal("receipt exposed source identifiers")
	}
	store.duplicate = true
	w = request("POST", "/api/v1/integration/results", "test-token", "application/json", raw)
	var reply struct {
		Duplicate bool `json:"duplicate"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 || !reply.Duplicate {
		t.Fatal("duplicate response incorrect")
	}
	w = request("GET", "/api/v1/integration/results/msg-test", "test-token", "", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatal("status endpoint failed")
	}
	if w := request("GET", "/api/v1/integration/results/missing", "test-token", "", nil); w.Code != 404 {
		t.Fatal("missing receipt not 404")
	}
	if w := request("POST", "/api/v1/integration/results", "test-token", "application/fhir+json", raw); w.Code != 415 {
		t.Fatal("intake confused with FHIR resource API")
	}
	if w := request("POST", "/api/v1/integration/results", "test-token", "application/json", []byte(`null`)); w.Code != 400 {
		t.Fatal("null accepted")
	}
	if w := request("POST", "/api/v1/integration/results", "test-token", "application/json", bytes.Repeat([]byte(" "), maxBodySize+1)); w.Code != 413 {
		t.Fatal("oversized body accepted")
	}
	store.failure = errors.New("secret patient value from database")
	w = request("POST", "/api/v1/integration/results", "test-token", "application/json", raw)
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("storage details leaked")
	}
}
