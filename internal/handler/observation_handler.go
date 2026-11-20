package handler

import (
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/service"
	"net/http"
	"strings"
)

type ObservationHandler struct{ service *service.ObservationService }

func NewObservationHandler(s *service.ObservationService) *ObservationHandler {
	return &ObservationHandler{service: s}
}

func (h *ObservationHandler) HandleObservation(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/Observation/"), "/")
	if len(parts) >= 2 && parts[1] == "_history" {
		h.handleObservationHistory(w, r, parts)
		return
	}
	// BEFORE: GET only read ?patient= and returned a search result.
	// NOW: requestID also lets GET /Observation/{id} retrieve one Observation.
	// The Patient handler has the longer annotated before/after request walkthrough;
	// these helpers perform the same HTTP tasks with Observation as the resource type.
	id, err := requestID(r, "Observation")
	if err != nil {
		sendError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			o, err := h.service.GetObservationByID(r.Context(), id)
			if err != nil {
				sendError(w, err)
				return
			}
			writeResource(w, r, http.StatusOK, o, "Observation", o.ID, o.Meta)
			return
		}
		bundle, err := h.service.GetObservationsForPatient(r.Context(), r.URL.Query().Get("patient"))
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
		// BEFORE: var o internal.Observation; json.NewDecoder(r.Body).Decode(&o)
		// NOW: reuse body/decoding checks, then call the unchanged service boundary.
		o, err := decodeResource[internal.Observation](w, r)
		if err != nil {
			sendError(w, err)
			return
		}
		created, err := h.service.CreateObservation(r.Context(), o)
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusCreated, created, "Observation", created.ID, created.Meta)

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
		var updated internal.Observation
		if r.Method == http.MethodPut {
			resource, decodeErr := decodeResource[internal.Observation](w, r)
			if decodeErr != nil {
				sendError(w, decodeErr)
				return
			}
			updated, err = h.service.UpdateObservation(r.Context(), id, resource, version)
		} else {
			data, readErr := readBody(w, r, "application/json-patch+json")
			if readErr != nil {
				sendError(w, readErr)
				return
			}
			updated, err = h.service.PatchObservation(r.Context(), id, data, version)
		}
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusOK, updated, "Observation", updated.ID, updated.Meta)
	case http.MethodDelete:
		version, err := ifMatch(r)
		if err != nil {
			sendError(w, err)
			return
		}
		if err := h.service.DeleteObservation(r.Context(), id, version); err != nil {
			sendError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, "GET, POST, PUT, PATCH, DELETE")
	}
}

func (h *ObservationHandler) handleObservationHistory(w http.ResponseWriter, r *http.Request, parts []string) {
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
		resource, meta, err := h.service.GetObservationVersion(r.Context(), id, parts[2])
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusOK, resource, "Observation", id, meta)
		return
	}
	bundle, err := h.service.GetObservationHistory(r.Context(), id)
	if err != nil {
		sendError(w, err)
		return
	}
	for i := range bundle.Entry {
		bundle.Entry[i].FullUrl = resourceURL(r, "Observation", id)
	}
	writeBundle(w, r, bundle)
}
