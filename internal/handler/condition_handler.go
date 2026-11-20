package handler

import (
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/service"
	"net/http"
	"strings"
)

type ConditionHandler struct{ service *service.ConditionService }

func NewConditionHandler(s *service.ConditionService) *ConditionHandler {
	return &ConditionHandler{service: s}
}

func (h *ConditionHandler) HandleCondition(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/Condition/"), "/")
	if len(parts) >= 2 && parts[1] == "_history" {
		h.handleConditionHistory(w, r, parts)
		return
	}
	// BEFORE: GET only read ?patient=; POST decoded directly inside this handler.
	// NOW: an ID can select one Condition, and HTTP checks/writers are shared.
	// This does not add a layer: http.go is another file in this same package.
	// See patient_handler.go for the expanded BEFORE/ NOW comparisons of that flow.
	id, err := requestID(r, "Condition")
	if err != nil {
		sendError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			c, err := h.service.GetConditionByID(r.Context(), id)
			if err != nil {
				sendError(w, err)
				return
			}
			writeResource(w, r, http.StatusOK, c, "Condition", c.ID, c.Meta)
			return
		}
		bundle, err := h.service.GetConditionsForPatient(r.Context(), r.URL.Query().Get("patient"))
		if err != nil {
			sendError(w, err)
			return
		}
		writeBundle(w, r, bundle)
	case http.MethodPost:
		if id != "" {
			methodNotAllowed(w, "GET, PUT, PATCH, DELETE")
			return
		}
		// BEFORE: var c internal.Condition; json.NewDecoder(r.Body).Decode(&c)
		// The generic call is optional reuse; the read limits and checks are behavior.
		c, err := decodeResource[internal.Condition](w, r)
		if err != nil {
			sendError(w, err)
			return
		}
		created, err := h.service.CreateCondition(r.Context(), c)
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusCreated, created, "Condition", created.ID, created.Meta)

	case http.MethodPut, http.MethodPatch:
		if id == "" {
			sendError(w, internal.Invalid("resource id is required"))
			return
		}
		version, err := ifMatch(r)
		if err != nil {
			sendError(w, err)
			return
		}
		var updated internal.Condition
		if r.Method == http.MethodPut {
			resource, decodeErr := decodeResource[internal.Condition](w, r)
			if decodeErr != nil {
				sendError(w, decodeErr)
				return
			}
			updated, err = h.service.UpdateCondition(r.Context(), id, resource, version)
		} else {
			data, readErr := readBody(w, r, "application/json-patch+json")
			if readErr != nil {
				sendError(w, readErr)
				return
			}
			updated, err = h.service.PatchCondition(r.Context(), id, data, version)
		}
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusOK, updated, "Condition", updated.ID, updated.Meta)
	case http.MethodDelete:
		version, err := ifMatch(r)
		if err != nil {
			sendError(w, err)
			return
		}
		if err := h.service.DeleteCondition(r.Context(), id, version); err != nil {
			sendError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, "GET, POST, PUT, PATCH, DELETE")
	}
}

func (h *ConditionHandler) handleConditionHistory(w http.ResponseWriter, r *http.Request, parts []string) {
	if (len(parts) != 2 && len(parts) != 3) || !internal.ValidID(parts[0]) {
		sendError(w, internal.Invalid("invalid history path"))
		return
	}
	id := parts[0]
	for _, name := range []string{"_count", "_since", "_at", "_list"} {
		if r.URL.Query().Has(name) {
			sendError(w, internal.Invalid("history filters and pagination are not supported yet"))
			return
		}
	}
	if queryID := r.URL.Query().Get("id"); queryID != "" && queryID != id {
		sendError(w, internal.Invalid("path and query ids disagree"))
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	if len(parts) == 3 {
		resource, meta, err := h.service.GetConditionVersion(r.Context(), id, parts[2])
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusOK, resource, "Condition", id, meta)
		return
	}
	bundle, err := h.service.GetConditionHistory(r.Context(), id)
	if err != nil {
		sendError(w, err)
		return
	}
	for i := range bundle.Entry {
		bundle.Entry[i].FullUrl = resourceURL(r, "Condition", id)
	}
	writeBundle(w, r, bundle)
}
