package middleware

import (
	"encoding/json"
	"go-fhir-phase1/internal"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth(t *testing.T) {
	for _, test := range []struct {
		name, secret, header string
		status               int
	}{
		{"missing", "test-token", "", 401},
		{"wrong token", "test-token", "Bearer wrong", 401},
		{"wrong scheme", "test-token", "Basic test-token", 401},
		{"extra fields", "test-token", "Bearer test-token extra", 401},
		{"empty config fails closed", "", "Bearer anything", 401},
		{"valid", "test-token", "Bearer test-token", 204},
		{"case insensitive scheme", "test-token", "bearer test-token", 204},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			h := AuthMiddleware(test.secret, func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })
			r := httptest.NewRequest("GET", "/api/v1/Patient", nil)
			r.Header.Set("Authorization", test.header)
			w := httptest.NewRecorder()
			h(w, r)
			if w.Code != test.status || called != (test.status == 204) {
				t.Fatalf("status=%d reached handler=%v", w.Code, called)
			}
			if test.status == 401 {
				var outcome internal.OperationOutcome
				if err := json.Unmarshal(w.Body.Bytes(), &outcome); err != nil {
					t.Fatal(err)
				}
				if outcome.Issue[0].Code == "" || w.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Fatalf("invalid auth response: %s", w.Body.String())
				}
			}
		})
	}
}

func TestRequestTimeoutPropagates(t *testing.T) {
	h := RequestTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("request has no deadline")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
