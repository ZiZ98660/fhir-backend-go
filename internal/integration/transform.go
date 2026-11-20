package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"go-fhir-phase1/internal"
)

// BuildTransactionBundle turns one accepted synthetic result into two linked
// FHIR resources. It does not invent a ServiceRequest or Specimen: intake only
// has an order mapping key and no specimen identity.
func BuildTransactionBundle(scope Scope, patientID string, result Result, issuedAt time.Time) (json.RawMessage, string, string, error) {
	if !internal.ValidID(patientID) {
		return nil, "", "", internal.Invalid("invalid mapped patient id")
	}
	issuedAt = issuedAt.UTC()
	issued := issuedAt.Format(time.RFC3339Nano)
	observationID := deterministicID(scope, result.ResultID, "Observation")
	reportID := deterministicID(scope, result.ResultID, "DiagnosticReport")
	observationURL := "urn:uuid:" + deterministicUUID(scope, result.ResultID, "Observation")
	reportURL := "urn:uuid:" + deterministicUUID(scope, result.ResultID, "DiagnosticReport")

	code := internal.CodeableConcept{
		Coding: []internal.Coding{{
			System: result.Test.System, Code: result.Test.Code, Display: result.Test.Display,
		}},
		Text: result.Test.Display,
	}
	patient := internal.Reference{Reference: "Patient/" + patientID}
	observation := internal.Observation{
		ResourceType: "Observation",
		ID:           observationID,
		Identifier: []internal.Identifier{{
			System: "https://example.org/synthetic-lis/results",
			Value:  result.ResultID,
		}},
		Status: "final",
		Category: []internal.CodeableConcept{{Coding: []internal.Coding{{
			System: "http://terminology.hl7.org/CodeSystem/observation-category",
			Code:   "laboratory", Display: "Laboratory",
		}}}},
		Code:              code,
		Subject:           patient,
		EffectiveDateTime: result.EffectiveDateTime,
		ValueQuantity: &internal.Quantity{
			Value: result.Value.Value, Unit: result.Value.Unit,
			System: result.Value.System, Code: result.Value.Code,
		},
	}
	if err := observation.Validate(); err != nil {
		return nil, "", "", err
	}

	report := internal.DiagnosticReport{
		ResourceType:      "DiagnosticReport",
		ID:                reportID,
		Status:            "final",
		Code:              code,
		Subject:           &patient,
		EffectiveDateTime: &result.EffectiveDateTime,
		Issued:            &issued,
		Result:            []internal.Reference{{Reference: observationURL}},
	}
	if err := report.Validate(); err != nil {
		return nil, "", "", err
	}

	// PUT and deterministic IDs let a receiving FHIR server repeat the bundle
	// without creating a second Observation or DiagnosticReport.
	bundle := internal.Bundle{
		ResourceType: "Bundle",
		Type:         "transaction",
		Entry: []internal.BundleEntry{
			{
				FullUrl:  observationURL,
				Resource: observation,
				Request:  &internal.BundleRequest{Method: "PUT", URL: "Observation/" + observationID},
			},
			{
				FullUrl:  reportURL,
				Resource: report,
				Request:  &internal.BundleRequest{Method: "PUT", URL: "DiagnosticReport/" + reportID},
			},
		},
	}

	data, err := json.Marshal(bundle)
	if err != nil {
		return nil, "", "", fmt.Errorf("marshal FHIR transaction bundle: %w", err)
	}
	return data, observationID, reportID, nil
}

func deterministicID(scope Scope, resultID, resourceType string) string {
	key := scope.TenantID + "\x1f" + scope.SourceID + "\x1f" + resultID + "\x1f" + resourceType
	sum := sha256.Sum256([]byte(key))
	prefix := "obs-"
	if resourceType == "DiagnosticReport" {
		prefix = "dr-"
	}
	return prefix + hex.EncodeToString(sum[:16])
}

func deterministicUUID(scope Scope, resultID, resourceType string) string {
	key := "bundle-reference\x1f" + scope.TenantID + "\x1f" + scope.SourceID + "\x1f" + resultID + "\x1f" + resourceType
	sum := sha256.Sum256([]byte(key))
	var id [16]byte
	copy(id[:], sum[:16])
	// Mark the value as a version-5-style UUID with the standard RFC variant bits.
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}
