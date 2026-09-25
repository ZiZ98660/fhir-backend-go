package handler

import (
	"encoding/json"
	"net/http"

	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/service"
)

// PatientHandler handles HTTP requests for Patients.
type PatientHandler struct {
	service *service.PatientService
}

// NewPatientHandler creates a new handler instance.
func NewPatientHandler(s *service.PatientService) *PatientHandler {
	return &PatientHandler{service: s}
}

// HandlePatient is the main router for /api/v1/Patient
func (h *PatientHandler) HandlePatient(w http.ResponseWriter, r *http.Request) {
	// Set the FHIR-specific content type
	w.Header().Set("Content-Type", "application/fhir+json")

	switch r.Method {
	case http.MethodGet:
		h.getPatient(w, r)
	case http.MethodPost:
		h.createPatient(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
	}
}

func (h *PatientHandler) getPatient(w http.ResponseWriter, r *http.Request) {
	patientID := r.URL.Query().Get("id")

	if patientID == "" {
		// Return all patients wrapped in a FHIR Bundle
		bundle, err := h.service.SearchPatients(r.Context())
		if err != nil {
			http.Error(w, `{"error": "failed to fetch patients"}`, http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(bundle)
		return
	}

	// Return specific patient
	patient, err := h.service.GetPatientByID(r.Context(), patientID)
	if err != nil {
		http.Error(w, `{"error": "patient not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(patient)
}

func (h *PatientHandler) createPatient(w http.ResponseWriter, r *http.Request) {
	var p internal.Patient
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, `{"error": "Invalid JSON payload"}`, http.StatusBadRequest)
		return
	}

	createdPatient, err := h.service.CreatePatient(r.Context(), p)
	if err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createdPatient)
}
