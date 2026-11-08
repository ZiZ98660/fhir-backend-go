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

type ConditionService struct{ repo repository.ConditionStore }

func NewConditionService(repo repository.ConditionStore) *ConditionService {
	return &ConditionService{repo: repo}
}

func (s *ConditionService) CreateCondition(ctx context.Context, c internal.Condition) (internal.Condition, error) {
	// BEFORE: check nonempty subject.reference and code.text here, then generate
	// fmt.Sprintf("cond-%d", time.Now().UnixNano()) and assign metadata inline.
	// NOW: reusable validation and creation defaults. The marshal/save flow remains.
	if err := c.Validate(); err != nil {
		return internal.Condition{}, err
	}
	c.ID, c.Meta = newID("cond"), newMeta()
	data, err := json.Marshal(c)
	if err != nil {
		return internal.Condition{}, err
	}
	if err := s.repo.SaveCondition(ctx, c, data); err != nil {
		return internal.Condition{}, err
	}
	return c, nil
}

func (s *ConditionService) GetConditionsForPatient(ctx context.Context, patient string) (internal.Bundle, error) {
	// BEFORE: always prefix the supplied query with "Patient/".
	// NOW: normalize first so Patient/p1 does not become Patient/Patient/p1.
	// The JSONB repository query still needs the full local reference, not just p1.
	id, err := internal.PatientSearchID(patient)
	if err != nil {
		return internal.Bundle{}, err
	}
	data, err := s.repo.GetConditionsByPatient(ctx, "Patient/"+id)
	if err != nil {
		return internal.Bundle{}, err
	}
	return searchBundle[internal.Condition](data)
}

func (s *ConditionService) GetConditionByID(ctx context.Context, id string) (internal.Condition, error) {
	// NEW BEHAVIOR: expose the repository's ID lookup as a readable instance URL,
	// with the same stored-data checks as the other resource services.
	if !internal.ValidID(id) {
		return internal.Condition{}, internal.Invalid("invalid condition id")
	}
	data, err := s.repo.GetConditionByID(ctx, id)
	if err != nil {
		return internal.Condition{}, err
	}
	return decodeStored[internal.Condition](data, id)
}

// BEFORE: this resource had create/read only. NOW: PUT preserves identity and
// records a new version; subject stays fixed so another patient's record cannot
// be silently reassigned during an edit.
func (s *ConditionService) UpdateCondition(ctx context.Context, id string, resource internal.Condition, expectedVersion string) (internal.Condition, error) {
	if !internal.ValidID(id) || resource.ID != id {
		return internal.Condition{}, internal.Invalid("body id must match the resource id")
	}
	current, err := s.GetConditionByID(ctx, id)
	if err != nil {
		return internal.Condition{}, err
	}
	if expectedVersion != "" && expectedVersion != current.Meta.VersionID {
		return internal.Condition{}, internal.ErrPrecondition
	}
	return s.replaceCondition(ctx, current, resource)
}

func (s *ConditionService) replaceCondition(ctx context.Context, current, resource internal.Condition) (internal.Condition, error) {
	if resource.ID != current.ID {
		return internal.Condition{}, internal.Invalid("resource id cannot change")
	}
	if resource.Subject.Reference != current.Subject.Reference {
		return internal.Condition{}, internal.Invalid("subject.reference cannot change on an existing Condition")
	}
	if err := resource.Validate(); err != nil {
		return internal.Condition{}, err
	}
	n := 0
	if current.Meta.VersionID != "" {
		var err error
		n, err = strconv.Atoi(current.Meta.VersionID)
		if err != nil || n < 1 || n == int(^uint(0)>>1) {
			return internal.Condition{}, fmt.Errorf("invalid stored Condition version")
		}
	}
	resource.Meta = internal.Meta{VersionID: strconv.Itoa(n + 1), LastUpdated: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(resource)
	if err != nil {
		return internal.Condition{}, err
	}
	if err := s.repo.UpdateCondition(ctx, resource, data, current.Meta.VersionID); err != nil {
		return internal.Condition{}, err
	}
	return resource, nil
}

func (s *ConditionService) PatchCondition(ctx context.Context, id string, patchData []byte, expectedVersion string) (internal.Condition, error) {
	patch, err := jsonpatch.DecodePatch(patchData)
	if err != nil || bytes.Equal(bytes.TrimSpace(patchData), []byte("null")) {
		return internal.Condition{}, internal.Invalid("invalid JSON Patch document")
	}
	current, err := s.GetConditionByID(ctx, id)
	if err != nil {
		return internal.Condition{}, err
	}
	if expectedVersion != "" && expectedVersion != current.Meta.VersionID {
		return internal.Condition{}, internal.ErrPrecondition
	}
	document, err := json.Marshal(current)
	if err != nil {
		return internal.Condition{}, err
	}
	options := jsonpatch.NewApplyOptions()
	options.SupportNegativeIndices = false
	options.AccumulatedCopySizeLimit = 1 << 20
	patched, err := patch.ApplyWithOptions(document, options)
	if err != nil || len(patched) > 1<<20 {
		return internal.Condition{}, internal.Invalid("patch failed or resource exceeds 1 MiB")
	}
	var resource internal.Condition
	decoder := json.NewDecoder(bytes.NewReader(patched))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&resource); err != nil {
		return internal.Condition{}, internal.Invalid("patch produced an unsupported resource: %v", err)
	}
	return s.replaceCondition(ctx, current, resource)
}

func (s *ConditionService) DeleteCondition(ctx context.Context, id, version string) error {
	if !internal.ValidID(id) {
		return internal.Invalid("invalid resource id")
	}
	return s.repo.DeleteCondition(ctx, id, version)
}

func (s *ConditionService) GetConditionHistory(ctx context.Context, id string) (internal.Bundle, error) {
	if !internal.ValidID(id) {
		return internal.Bundle{}, internal.Invalid("invalid resource id")
	}
	versions, err := s.repo.GetConditionHistory(ctx, id)
	if err != nil {
		return internal.Bundle{}, err
	}
	return resourceHistoryBundle(versions, id, "Condition")
}

func (s *ConditionService) GetConditionVersion(ctx context.Context, id, version string) (json.RawMessage, internal.Meta, error) {
	if !internal.ValidID(id) || !internal.ValidID(version) {
		return nil, internal.Meta{}, internal.Invalid("invalid resource id or version")
	}
	entry, err := s.repo.GetConditionVersion(ctx, id, version)
	if err != nil {
		return nil, internal.Meta{}, err
	}
	if entry.Method == "DELETE" {
		return nil, internal.Meta{}, internal.ErrGone
	}
	return resourceVersionResource(entry, id, "Condition")
}
