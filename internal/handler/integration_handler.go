package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/integration"
)

// IntegrationHandler exposes an application JSON contract, not a FHIR endpoint.
type IntegrationHandler struct {
	service *integration.Service
	scope   integration.Scope
}

func NewIntegrationHandler(service *integration.Service, scope integration.Scope) *IntegrationHandler {
	return &IntegrationHandler{service: service, scope: scope}
}

func (h *IntegrationHandler) HandleResults(w http.ResponseWriter, r *http.Request) {
	const base = "/api/v1/integration/results"
	path := strings.TrimPrefix(r.URL.Path, base)
	if path == "" || path == "/" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		data, err := readBody(w, r, "application/json")
		if err != nil {
			sendError(w, err)
			return
		}
		receipt, duplicate, err := h.service.Submit(r.Context(), h.scope, data)
		if err != nil {
			integrationError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", base+"/"+receipt.ID)
		if duplicate {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
		_ = json.NewEncoder(w).Encode(struct {
			integration.Receipt
			Duplicate bool `json:"duplicate"`
		}{receipt, duplicate})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	id := strings.TrimPrefix(path, "/")
	receipt, err := h.service.Get(r.Context(), h.scope, id)
	if err != nil {
		integrationError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(receipt)
}

func integrationError(w http.ResponseWriter, err error) {
	if errors.Is(err, internal.ErrInvalid) || errors.Is(err, internal.ErrConflict) ||
		errors.Is(err, internal.ErrNotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		sendError(w, err)
		return
	}
	// Database errors can include patient identifiers. Never pass them to the
	// existing generic logger. A future structured logger can add safe categories.
	internal.WriteOutcome(w, http.StatusInternalServerError, "exception", "integration storage unavailable")
}
