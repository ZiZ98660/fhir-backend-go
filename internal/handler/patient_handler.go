package handler

import (
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/service"
	"net/http"
	"strings"
)

type PatientHandler struct{ service *service.PatientService }

func NewPatientHandler(s *service.PatientService) *PatientHandler { return &PatientHandler{service: s} }

// LEARNING ROUTE: read POST first, then GET, then PUT/PATCH, then DELETE.
// The handler still coordinates one request; helpers are ordinary function calls.
// Compare the BEFORE excerpts here with the live code immediately below them.
func (h *PatientHandler) HandlePatient(w http.ResponseWriter, r *http.Request) {
	// History paths have more segments than an ordinary instance path. Keep this
	// routing explicit instead of introducing a general-purpose routing abstraction.
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/Patient/"), "/")
	if len(parts) >= 2 && parts[1] == "_history" {
		h.handlePatientHistory(w, r, parts)
		return
	}
	// BEFORE: patientID := r.URL.Query().Get("id")
	// NOW: one parser handles both path IDs and the old query form, with checks.
	id, err := requestID(r, "Patient")
	if err != nil {
		sendError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			p, err := h.service.GetPatientByID(r.Context(), id)
			if err != nil {
				sendError(w, err)
				return
			}
			// BEFORE: json.NewEncoder(w).Encode(patient)
			// NOW: the shared writer also supplies version/timestamp headers.
			writeResource(w, r, http.StatusOK, p, "Patient", p.ID, p.Meta)
			return
		}
		bundle, err := h.service.SearchPatients(r.Context(), r.URL.Query().Get("identifier"), r.URL.Query().Get("search"))
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
		// BEFORE (excerpt; inactive):
		// var p internal.Patient
		// if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		//     h.sendOutcome(w, http.StatusBadRequest, "error", "invalid JSON")
		//     return
		// }
		// NOW: the type argument selects Patient; http.go contains the read checks.
		// The helper is optional. Keeping all those checks here would also work.
		p, err := decodeResource[internal.Patient](w, r)
		if err != nil {
			sendError(w, err)
			return
		}
		// This step did not become another abstraction: the handler still calls
		// the same service to validate and save, passing this request's context.
		created, err := h.service.CreatePatient(r.Context(), p)
		if err != nil {
			sendError(w, err)
			return
		}
		// BEFORE: w.WriteHeader(http.StatusCreated); json.NewEncoder(w).Encode(createdPatient)
		// NOW: the writer adds consistent response headers and the resource URL.
		writeResource(w, r, http.StatusCreated, created, "Patient", created.ID, created.Meta)
	case http.MethodPut, http.MethodPatch:
		// BEFORE (excerpt from the former combined branch):
		// case http.MethodPost, http.MethodPut, http.MethodPatch:
		//     createdPatient, err := h.service.CreatePatient(r.Context(), p)
		//
		// BUG FIX, not just extraction: calling CreatePatient generated a new ID
		// for PUT/PATCH. Now PUT supplies a replacement and PATCH supplies operations;
		// both target an existing ID. No helper or generic was required to fix that.
		if id == "" {
			sendError(w, internal.Invalid("patient id is required in the URL"))
			return
		}
		version, err := ifMatch(r)
		if err != nil {
			sendError(w, err)
			return
		}
		var updated internal.Patient
		if r.Method == http.MethodPut {
			p, decodeErr := decodeResource[internal.Patient](w, r)
			if decodeErr != nil {
				sendError(w, decodeErr)
				return
			}
			updated, err = h.service.UpdatePatient(r.Context(), id, p, version)
		} else {
			data, readErr := readBody(w, r, "application/json-patch+json")
			if readErr != nil {
				sendError(w, readErr)
				return
			}
			updated, err = h.service.PatchPatient(r.Context(), id, data, version)
		}
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusOK, updated, "Patient", updated.ID, updated.Meta)
	case http.MethodDelete:
		// BEFORE: DELETE removed the database row. NOW: mark it deleted and save
		// a history marker. Old versions stay readable through /_history.
		version, err := ifMatch(r)
		if err != nil {
			sendError(w, err)
			return
		}
		if err := h.service.DeletePatient(r.Context(), id, version); err != nil {
			sendError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w, "GET, POST, PUT, PATCH, DELETE")
	}
}

func (h *PatientHandler) handlePatientHistory(w http.ResponseWriter, r *http.Request, parts []string) {
	if (len(parts) != 2 && len(parts) != 3) || !internal.ValidID(parts[0]) {
		sendError(w, internal.Invalid("expected Patient/{id}/_history or Patient/{id}/_history/{version}"))
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
		resource, meta, err := h.service.GetPatientVersion(r.Context(), id, parts[2])
		if err != nil {
			sendError(w, err)
			return
		}
		writeResource(w, r, http.StatusOK, resource, "Patient", id, meta)
		return
	}
	bundle, err := h.service.GetPatientHistory(r.Context(), id)
	if err != nil {
		sendError(w, err)
		return
	}
	for i := range bundle.Entry {
		bundle.Entry[i].FullUrl = resourceURL(r, "Patient", id)
	}
	writeBundle(w, r, bundle)
}
