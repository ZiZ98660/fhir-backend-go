package internal

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestAuthoredR4LaboratoryFixtures(t *testing.T) {
	for name, target := range map[string]interface{ Validate() error }{
		"servicerequest": &ServiceRequest{}, "specimen": &Specimen{},
		"diagnosticreport": &DiagnosticReport{}, "observation": &Observation{},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../testdata/fhir-r4/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(target); err != nil {
				t.Fatal(err)
			}
			if err := target.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLabResourceBaseCardinalitiesAndShapes(t *testing.T) {
	// Base R4 does not require a Specimen status/subject or a report subject.
	if err := (Specimen{ResourceType: "Specimen"}).Validate(); err != nil {
		t.Fatal(err)
	}
	report := DiagnosticReport{ResourceType: "DiagnosticReport", Status: "final", Code: CodeableConcept{Text: "Synthetic report"}}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	request := ServiceRequest{ResourceType: "ServiceRequest", Status: "active", Intent: "order", Subject: Reference{"Patient/p1"}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	request.Subject.Reference = ""
	if err := request.Validate(); err == nil {
		t.Fatal("ServiceRequest subject was not required")
	}

	observation := Observation{Specimen: &Reference{"Specimen/s1"}, BasedOn: []Reference{{"ServiceRequest/o1"}}}
	report.Specimen, report.Result = []Reference{{"Specimen/s1"}}, []Reference{{"Observation/r1"}}
	obsJSON, _ := json.Marshal(observation)
	reportJSON, _ := json.Marshal(report)
	if !strings.Contains(string(obsJSON), `"specimen":{"reference":"Specimen/s1"}`) {
		t.Fatal("Observation.specimen must be an object")
	}
	if !strings.Contains(string(reportJSON), `"specimen":[{"reference":"Specimen/s1"}]`) {
		t.Fatal("DiagnosticReport.specimen must be an array")
	}
	if !strings.Contains(string(reportJSON), `"result":[{"reference":"Observation/r1"}]`) {
		t.Fatal("report results must reference Observations")
	}
	encoded, _ := json.Marshal(Specimen{ResourceType: "Specimen"})
	if string(encoded) != `{"resourceType":"Specimen"}` {
		t.Fatalf("absent optional fields leaked: %s", encoded)
	}
}

func TestLabResourceSubsetRejectsInvalidStatusesLinksAndTimes(t *testing.T) {
	empty, badStatus, partial, full := "", "final", "2026-11", "2026-11-08T09:00:00+01:00"
	for name, resource := range map[string]interface{ Validate() error }{
		"specimen status":       Specimen{ResourceType: "Specimen", Status: &badStatus},
		"empty status":          Specimen{ResourceType: "Specimen", Status: &empty},
		"wrong request target":  Specimen{ResourceType: "Specimen", Request: []Reference{{"Observation/o1"}}},
		"report missing code":   DiagnosticReport{ResourceType: "DiagnosticReport", Status: "final"},
		"report missing status": DiagnosticReport{ResourceType: "DiagnosticReport", Code: CodeableConcept{Text: "test"}},
		"issued partial":        DiagnosticReport{ResourceType: "DiagnosticReport", Status: "final", Code: CodeableConcept{Text: "test"}, Issued: &partial},
	} {
		t.Run(name, func(t *testing.T) {
			if resource.Validate() == nil {
				t.Fatal("invalid resource accepted")
			}
		})
	}
	report := DiagnosticReport{ResourceType: "DiagnosticReport", Status: "appended", Code: CodeableConcept{Text: "test"}, EffectiveDateTime: &partial, Issued: &full}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSupportedLabTimestampLexicalBoundaries(t *testing.T) {
	for _, value := range []string{"2026-11-08T09:00:00Z", "2026-11-08T09:00:00.123456789+14:00"} {
		if !ValidLabInstant(value) {
			t.Fatalf("supported timestamp rejected: %s", value)
		}
	}
	for _, value := range []string{
		"0000-11-08T09:00:00Z", "2026-11-08T9:00:00Z", "2026-11-08T09:00:00,5Z",
		"2026-11-08T09:00:00+15:00", "2026-11-08T09:00:00+14:01",
		"2026-11-08T09:00:00.1234567891Z", "2026-02-30T09:00:00Z", "2026-11-08",
	} {
		if ValidLabInstant(value) {
			t.Fatalf("unsupported timestamp accepted: %s", value)
		}
	}
}

func TestLabIdentifiersAndObservationLinkTargets(t *testing.T) {
	for _, value := range []interface{ Validate() error }{
		ServiceRequest{ResourceType: "ServiceRequest", Status: "active", Intent: "order", Subject: Reference{"Patient/p1"}, Identifier: []Identifier{{}}},
		Specimen{ResourceType: "Specimen", Identifier: []Identifier{{}}},
		DiagnosticReport{ResourceType: "DiagnosticReport", Status: "final", Code: CodeableConcept{Text: "test"}, Identifier: []Identifier{{}}},
	} {
		if value.Validate() == nil {
			t.Fatal("empty identifier accepted")
		}
	}
	for _, specimen := range []Reference{{}, {"Patient/p2"}} {
		obs := Observation{ResourceType: "Observation", Status: "final", Code: CodeableConcept{Text: "test"}, Subject: Reference{"Patient/p1"}, Specimen: &specimen}
		if obs.Validate() == nil {
			t.Fatal("wrong specimen reference accepted")
		}
	}
}
