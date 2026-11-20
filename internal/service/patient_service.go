package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/repository"
	"strconv"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

type PatientService struct{ repo repository.PatientStore }

func NewPatientService(repo repository.PatientStore) *PatientService {
	return &PatientService{repo: repo}
}

// BEFORE (the simpler pre-cleanup CreatePatient, kept inactive for comparison):
//
//	func (s *PatientService) CreatePatient(ctx context.Context, p internal.Patient) (internal.Patient, error) {
//	    if len(p.Name) == 0 || p.Name[0].Family == "" {
//	        return internal.Patient{}, errors.New("at least one name with a family name is required")
//	    }
//	    p.ID = fmt.Sprintf("pt-%d", time.Now().UnixNano())
//	    p.Meta = internal.Meta{
//	        VersionID: "1",
//	        LastUpdated: time.Now().Format(time.RFC3339),
//	    }
//	    data, err := json.Marshal(p)
//	    if err != nil { return internal.Patient{}, err }
//	    err = s.repo.SavePatient(ctx, p, data)
//	    if err != nil { return internal.Patient{}, err }
//	    return p, nil
//	}
//
// NOW: Validate owns the supported checks; newID/newMeta share creation defaults.
// Requiring a family name was our old application restriction, not a universal
// Patient requirement. The marshal -> repository -> return flow remains.
// OPTIONAL ABSTRACTION: those helpers could be explicit code here. The behavior
// changes are the validation rules and collision-resistant ID generation.
func (s *PatientService) CreatePatient(ctx context.Context, p internal.Patient) (internal.Patient, error) {
	if err := p.Validate(); err != nil {
		return internal.Patient{}, err
	}
	p.ID, p.Meta = newID("pt"), newMeta()
	data, err := json.Marshal(p)
	if err != nil {
		return internal.Patient{}, err
	}
	if err := s.repo.SavePatient(ctx, p, data); err != nil {
		return internal.Patient{}, err
	}
	return p, nil
}

// PUT replaces an existing patient. This server does not support update-as-create.
// BEFORE: there was no update service; the handler called CreatePatient.
// That assigned a new ID, so it was not an update of the addressed patient.
// NOW: check matching IDs -> validate -> read the current patient -> compare the
// client's optional version -> prepare and perform a version-checked write.
// These steps are update behavior, not a requirement to use abstractions.
func (s *PatientService) UpdatePatient(ctx context.Context, id string, p internal.Patient, version string) (internal.Patient, error) {
	if !internal.ValidID(id) {
		return internal.Patient{}, internal.Invalid("valid patient id is required")
	}
	if p.ID != id {
		return internal.Patient{}, internal.Invalid("body id must match the patient id in the URL")
	}
	if err := p.Validate(); err != nil {
		return internal.Patient{}, err
	}
	current, err := s.GetPatientByID(ctx, id)
	if err != nil {
		return internal.Patient{}, err
	}
	if version != "" && version != current.Meta.VersionID {
		return internal.Patient{}, internal.ErrPrecondition
	}
	return s.replacePatient(ctx, current, p)
}

// BEFORE: no shared final update operation existed.
// NOW: PUT and PATCH both finish here with a full Patient value to save.
// WHY EXTRACT: both must preserve identity, validate, increment the version,
// marshal, and perform a conditional write. Sharing avoids two sets of rules.
// This private method is optional organization, not another service layer.
// current is the old patient; p is the proposed replacement. Atoi converts the
// stored version to an integer; Itoa converts the incremented version to a string.
// The repository receives the OLD version separately as a write condition.
// The guards also protect PATCH, which calls this method directly. PUT currently
// checks IDs/validation twice; abstractions can introduce some repetition too.
func (s *PatientService) replacePatient(ctx context.Context, current, p internal.Patient) (internal.Patient, error) {
	if p.ID != current.ID {
		return internal.Patient{}, internal.Invalid("body id must match the patient id in the URL")
	}
	if err := p.Validate(); err != nil {
		return internal.Patient{}, err
	}
	n := 0
	if current.Meta.VersionID != "" {
		var err error
		n, err = strconv.Atoi(current.Meta.VersionID)
		if err != nil || n < 1 || n == int(^uint(0)>>1) {
			return internal.Patient{}, fmt.Errorf("invalid stored patient version")
		}
	}
	p.Meta = internal.Meta{VersionID: strconv.Itoa(n + 1), LastUpdated: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(p)
	if err != nil {
		return internal.Patient{}, err
	}
	if err := s.repo.UpdatePatient(ctx, p, data, current.Meta.VersionID); err != nil {
		return internal.Patient{}, err
	}
	return p, nil
}

// BEFORE: PATCH decoded a Patient body and called CreatePatient, just like POST.
// There was no patch algorithm to restore as a simpler previous function.
// NOW: decode instructions -> read Patient -> check version -> apply instructions
// to its JSON -> decode the result -> call replacePatient. Unmentioned fields stay.
// WHY THE LIBRARY: json-patch supplies the patch algorithm; we still own identity,
// validation, versioning, and saving. Negative indexes are disabled and copy/output
// sizes are bounded. Implementing PATCH adds behavior; it is not just extraction.
func (s *PatientService) PatchPatient(ctx context.Context, id string, patchData []byte, version string) (internal.Patient, error) {
	patch, err := jsonpatch.DecodePatch(patchData)
	if err != nil || bytes.Equal(bytes.TrimSpace(patchData), []byte("null")) {
		return internal.Patient{}, internal.Invalid("invalid JSON Patch document")
	}
	current, err := s.GetPatientByID(ctx, id)
	if err != nil {
		return internal.Patient{}, err
	}
	if version != "" && version != current.Meta.VersionID {
		return internal.Patient{}, internal.ErrPrecondition
	}
	document, err := json.Marshal(current)
	if err != nil {
		return internal.Patient{}, err
	}
	options := jsonpatch.NewApplyOptions()
	options.SupportNegativeIndices = false
	options.AccumulatedCopySizeLimit = 1 << 20
	patched, err := patch.ApplyWithOptions(document, options)
	if err != nil {
		return internal.Patient{}, internal.Invalid("JSON Patch could not be applied")
	}
	if len(patched) > 1<<20 {
		return internal.Patient{}, internal.Invalid("patched patient is too large")
	}
	var p internal.Patient
	decoder := json.NewDecoder(bytes.NewReader(patched))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return internal.Patient{}, internal.Invalid("patch produced an unsupported patient: %v", err)
	}
	// Carry the version of the patched document into the atomic repository update.
	return s.replacePatient(ctx, current, p)
}

func (s *PatientService) SearchPatients(ctx context.Context, identifier, searchTerm string) (internal.Bundle, error) {
	data, err := s.repo.SearchPatients(ctx, identifier, searchTerm)
	if err != nil {
		return internal.Bundle{}, err
	}
	// BEFORE (simpler loop excerpt):
	// for _, data := range dataList {
	//     var p internal.Patient
	//     if err := json.Unmarshal(data, &p); err != nil { continue }
	//     bundle.Entry = append(bundle.Entry, internal.BundleEntry{Resource: p})
	// }
	// NOW: share the validated loop across resource types. Generics remove repeated
	// loops; failing on corrupted data instead of skipping it is a separate choice.
	return searchBundle[internal.Patient](data)
}

func (s *PatientService) GetPatientByID(ctx context.Context, id string) (internal.Patient, error) {
	if !internal.ValidID(id) {
		return internal.Patient{}, internal.Invalid("invalid patient id")
	}
	data, err := s.repo.GetPatientByID(ctx, id)
	if err != nil {
		return internal.Patient{}, err
	}
	// BEFORE:
	// var p internal.Patient
	// if err := json.Unmarshal(data, &p); err != nil { return internal.Patient{}, err }
	// return p, nil
	// NOW: also reject unsupported fields, null, invalid values, and mismatched IDs.
	// Sharing the implementation lets searches apply the same stored-data checks.
	return decodeStored[internal.Patient](data, id)
}

func (s *PatientService) DeletePatient(ctx context.Context, id, expectedVersion string) error {
	if !internal.ValidID(id) {
		return internal.Invalid("valid patient id is required")
	}
	return s.repo.DeletePatient(ctx, id, expectedVersion)
}

func (s *PatientService) GetPatientHistory(ctx context.Context, id string) (internal.Bundle, error) {
	if !internal.ValidID(id) {
		return internal.Bundle{}, internal.Invalid("invalid patient id")
	}
	versions, err := s.repo.GetPatientHistory(ctx, id)
	if err != nil {
		return internal.Bundle{}, err
	}
	return resourceHistoryBundle(versions, id, "Patient")
}

func (s *PatientService) GetPatientVersion(ctx context.Context, id, version string) (json.RawMessage, internal.Meta, error) {
	if !internal.ValidID(id) || !internal.ValidID(version) {
		return nil, internal.Meta{}, internal.Invalid("invalid patient id or version")
	}
	entry, err := s.repo.GetPatientVersion(ctx, id, version)
	if err != nil {
		return nil, internal.Meta{}, err
	}
	if entry.Method == "DELETE" {
		return nil, internal.Meta{}, internal.ErrGone
	}
	return resourceVersionResource(entry, id, "Patient")
}
