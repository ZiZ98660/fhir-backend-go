package service

import (
	"encoding/json"
	"errors"
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/repository"
	"testing"
	"time"
)

func TestStoredResourcesFailWithoutLosingData(t *testing.T) {
	for _, data := range []string{
		`null`,
		`{`,
		`{"resourceType":"Patient","id":"p1","unknown":true}`,
		`{"resourceType":"Patient"}`,
		`{"resourceType":"Observation","id":"p1"}`,
		`{"resourceType":"Patient","id":"p1","gender":"invalid"}`,
		`{"resourceType":"Patient","id":"p1"} {}`,
	} {
		if _, err := decodeStored[internal.Patient]([]byte(data), ""); err == nil || errors.Is(err, internal.ErrInvalid) {
			t.Errorf("stored %s: want server error, got %v", data, err)
		}
		if _, err := searchBundle[internal.Patient]([][]byte{[]byte(data)}); err == nil {
			t.Errorf("corrupt resource silently included: %s", data)
		}
	}
	if _, err := decodeStored[internal.Patient]([]byte(`{"resourceType":"Patient","id":"p1"}`), "p2"); err == nil {
		t.Fatal("mismatched database id accepted")
	}
}

func TestHistoricalSnapshotPreservesUnknownFieldsAndNumbers(t *testing.T) {
	entry := repository.ResourceVersion{VersionID: "7", Method: "PUT", RecordedAt: time.Now().UTC(), Data: []byte(`{"resourceType":"Patient","id":"p1","extra":{"large":9007199254740993},"meta":{"versionId":"7","lastUpdated":"2026-11-01T01:00:00Z","profile":["urn:test:profile"]}}`)}
	data, meta, err := resourceVersionResource(entry, "p1", "Patient")
	if err != nil {
		t.Fatal(err)
	}
	if meta.VersionID != "7" || meta.LastUpdated != "2026-11-01T01:00:00Z" {
		t.Fatalf("wrong metadata: %+v", meta)
	}
	var original, actual map[string]json.RawMessage
	if err := json.Unmarshal(entry.Data, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if string(actual["extra"]) != string(original["extra"]) || string(actual["meta"]) != `{"lastUpdated":"2026-11-01T01:00:00Z","profile":["urn:test:profile"],"versionId":"7"}` {
		t.Fatalf("historical fields were lost: %s", data)
	}
	entry.Data = []byte(`{"resourceType":"Patient","id":"p1"}`)
	entry.VersionID = "legacy"
	_, meta, err = resourceVersionResource(entry, "p1", "Patient")
	if err != nil || meta.VersionID != "legacy" || meta.LastUpdated == "" {
		t.Fatalf("legacy metadata: %+v %v", meta, err)
	}
	if _, _, err := resourceVersionResource(entry, "wrong-id", "Patient"); err == nil {
		t.Fatal("mismatched historical identity accepted")
	}
}
