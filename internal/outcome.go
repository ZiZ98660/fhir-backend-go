package internal

import (
	"encoding/json"
	"net/http"
)

// BEFORE (earlier handler helper, excerpt; inactive):
// w.WriteHeader(status)
//
//	outcome := internal.OperationOutcome{
//	    ResourceType: "OperationOutcome",
//	    Issue: []internal.Issue{{Severity: severity, Diagnostics: diagnostics}},
//	}
//
// json.NewEncoder(w).Encode(outcome)
//
// NOW: one writer sets content type, status, and required issue code, then encodes.
// Both handlers and auth middleware use it. handler.sendError chooses the category;
// this function writes the selected response. Shared internal placement lets
// middleware use it without importing handler. A separate file is optional.
func WriteOutcome(w http.ResponseWriter, status int, code, diagnostics string) {
	w.Header().Set("Content-Type", "application/fhir+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(OperationOutcome{
		ResourceType: "OperationOutcome",
		Issue:        []Issue{{Severity: "error", Code: code, Diagnostics: diagnostics}},
	})
}
