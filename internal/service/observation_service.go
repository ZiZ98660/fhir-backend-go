package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/repository"
	"strconv"
	"time"
)

type ObservationService struct{ repo repository.ObservationStore }

func NewObservationService(repo repository.ObservationStore) *ObservationService {
	return &ObservationService{repo: repo}
}

func (s *ObservationService) CreateObservation(ctx context.Context, o internal.Observation) (internal.Observation, error) {
	// BEFORE (validation excerpt):
	// if o.Subject.Reference == "" { return internal.Observation{}, errors.New("observation must have a subject reference") }
	// if o.Code.Text == "" { return internal.Observation{}, errors.New("observation must have a code text") }
	// NOW: Validate checks status/reference rules and accepts coding or text.
	// Moving rules into a method is optional; correcting/expanding them is behavior.
	if err := o.Validate(); err != nil {
		return internal.Observation{}, err
	}
	// BEFORE: o.ID = "obs-fhir-123"; o.Meta.VersionID = "1"
	// BUG FIX: each new observation gets a new ID. Helpers share creation defaults.
	o.ID, o.Meta = newID("obs"), newMeta()
	data, err := json.Marshal(o)
	if err != nil {
		return internal.Observation{}, err
	}
	if err := s.repo.SaveObservation(ctx, o, data); err != nil {
		return internal.Observation{}, err
	}
	return o, nil
}

func (s *ObservationService) GetObservationsForPatient(ctx context.Context, patient string) (internal.Bundle, error) {
	// BEFORE: pass the patient query straight to the repository.
	// NOW: normalize p1 or Patient/p1 to the ID needed by its patient_id column query.
	id, err := internal.PatientSearchID(patient)
	if err != nil {
		return internal.Bundle{}, err
	}
	data, err := s.repo.GetObservationsByPatient(ctx, id)
	if err != nil {
		return internal.Bundle{}, err
	}
	return searchBundle[internal.Observation](data)
}

func (s *ObservationService) GetObservationByID(ctx context.Context, id string) (internal.Observation, error) {
	// NEW BEHAVIOR: the repository had this lookup, but the service/handler now
	// expose it so an Observation instance URL can actually retrieve the record.
	if !internal.ValidID(id) {
		return internal.Observation{}, internal.Invalid("invalid observation id")
	}
	data, err := s.repo.GetObservationByID(ctx, id)
	if err != nil {
		return internal.Observation{}, err
	}
	return decodeStored[internal.Observation](data, id)
}

// BEFORE: this resource had create/read only. NOW: PUT preserves identity and
// records a new version; subject stays fixed so another patient's record cannot
// be silently reassigned during an edit.
func (s *ObservationService) UpdateObservation(ctx context.Context, id string, resource internal.Observation, expectedVersion string) (internal.Observation, error) {
	if !internal.ValidID(id) || resource.ID != id {
		return internal.Observation{}, internal.Invalid("body id must match the resource id")
	}
	current, err := s.GetObservationByID(ctx, id)
	if err != nil {
		return internal.Observation{}, err
	}
	if expectedVersion != "" && expectedVersion != current.Meta.VersionID {
		return internal.Observation{}, internal.ErrPrecondition
	}
	return s.replaceObservation(ctx, current, resource)
}

func (s *ObservationService) replaceObservation(ctx context.Context, current, resource internal.Observation) (internal.Observation, error) {
	if resource.ID != current.ID {
		return internal.Observation{}, internal.Invalid("resource id cannot change")
	}
	if resource.Subject.Reference != current.Subject.Reference {
		return internal.Observation{}, internal.Invalid("subject.reference cannot change on an existing Observation")
	}
	if err := resource.Validate(); err != nil {
		return internal.Observation{}, err
	}
	n := 0
	if current.Meta.VersionID != "" {
		var err error
		n, err = strconv.Atoi(current.Meta.VersionID)
		if err != nil || n < 1 || n == int(^uint(0)>>1) {
			return internal.Observation{}, fmt.Errorf("invalid stored Observation version")
		}
	}
	resource.Meta = internal.Meta{VersionID: strconv.Itoa(n + 1), LastUpdated: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(resource)
	if err != nil {
		return internal.Observation{}, err
	}
	if err := s.repo.UpdateObservation(ctx, resource, data, current.Meta.VersionID); err != nil {
		return internal.Observation{}, err
	}
	return resource, nil
}

func (s *ObservationService) PatchObservation(ctx context.Context, id string, patchData []byte, expectedVersion string) (internal.Observation, error) {
	patch, err := jsonpatch.DecodePatch(patchData)
	if err != nil || bytes.Equal(bytes.TrimSpace(patchData), []byte("null")) {
		return internal.Observation{}, internal.Invalid("invalid JSON Patch document")
	}
	current, err := s.GetObservationByID(ctx, id)
	if err != nil {
		return internal.Observation{}, err
	}
	if expectedVersion != "" && expectedVersion != current.Meta.VersionID {
		return internal.Observation{}, internal.ErrPrecondition
	}
	document, err := json.Marshal(current)
	if err != nil {
		return internal.Observation{}, err
	}
	options := jsonpatch.NewApplyOptions()
	options.SupportNegativeIndices = false
	options.AccumulatedCopySizeLimit = 1 << 20
	patched, err := patch.ApplyWithOptions(document, options)
	if err != nil || len(patched) > 1<<20 {
		return internal.Observation{}, internal.Invalid("patch failed or resource exceeds 1 MiB")
	}
	var resource internal.Observation
	decoder := json.NewDecoder(bytes.NewReader(patched))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&resource); err != nil {
		return internal.Observation{}, internal.Invalid("patch produced an unsupported resource: %v", err)
	}
	return s.replaceObservation(ctx, current, resource)
}

func (s *ObservationService) DeleteObservation(ctx context.Context, id, version string) error {
	if !internal.ValidID(id) {
		return internal.Invalid("invalid resource id")
	}
	return s.repo.DeleteObservation(ctx, id, version)
}

func (s *ObservationService) GetObservationHistory(ctx context.Context, id string) (internal.Bundle, error) {
	if !internal.ValidID(id) {
		return internal.Bundle{}, internal.Invalid("invalid resource id")
	}
	versions, err := s.repo.GetObservationHistory(ctx, id)
	if err != nil {
		return internal.Bundle{}, err
	}
	return resourceHistoryBundle(versions, id, "Observation")
}

func (s *ObservationService) GetObservationVersion(ctx context.Context, id, version string) (json.RawMessage, internal.Meta, error) {
	if !internal.ValidID(id) || !internal.ValidID(version) {
		return nil, internal.Meta{}, internal.Invalid("invalid resource id or version")
	}
	entry, err := s.repo.GetObservationVersion(ctx, id, version)
	if err != nil {
		return nil, internal.Meta{}, err
	}
	if entry.Method == "DELETE" {
		return nil, internal.Meta{}, internal.ErrGone
	}
	return resourceVersionResource(entry, id, "Observation")
}
