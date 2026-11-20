package integration

import (
	"encoding/json"
	"testing"
	"time"

	"go-fhir-phase1/internal"
)

func TestBuildTransactionBundle(t *testing.T) {
	raw := sample(t)
	result, err := DecodeResult(raw)
	if err != nil {
		t.Fatalf("failed to decode sample: %v", err)
	}

	scope := Scope{TenantID: "synthetic-demo", SourceID: "synthetic-lis"}
	patientID := "p1"
	issuedAt := time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)

	bundleBytes, obsID, reportID, err := BuildTransactionBundle(scope, patientID, result, issuedAt)
	if err != nil {
		t.Fatalf("BuildTransactionBundle failed: %v", err)
	}

	if obsID == "" || reportID == "" {
		t.Fatalf("expected non-empty resource IDs, got obs=%q, report=%q", obsID, reportID)
	}

	var bundle internal.Bundle
	if err := json.Unmarshal(bundleBytes, &bundle); err != nil {
		t.Fatalf("failed to unmarshal generated bundle: %v", err)
	}

	if bundle.ResourceType != "Bundle" || bundle.Type != "transaction" {
		t.Fatalf("invalid bundle type: %+v", bundle)
	}

	if len(bundle.Entry) != 2 {
		t.Fatalf("expected 2 entries in bundle, got %d", len(bundle.Entry))
	}

	// Verify deterministic IDs on repeat
	bundleBytes2, obsID2, reportID2, err := BuildTransactionBundle(scope, patientID, result, issuedAt)
	if err != nil {
		t.Fatalf("BuildTransactionBundle repeat failed: %v", err)
	}
	if obsID != obsID2 || reportID != reportID2 || string(bundleBytes) != string(bundleBytes2) {
		t.Fatalf("transformation is not deterministic")
	}

	// Check invalid patient
	if _, _, _, err := BuildTransactionBundle(scope, "invalid id with spaces", result, issuedAt); err == nil {
		t.Fatalf("expected error for invalid patient id")
	}
}
