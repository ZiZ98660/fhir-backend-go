package internal

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOptionalValuesRoundTrip(t *testing.T) {
	var patient Patient
	if err := json.Unmarshal([]byte(`{"resourceType":"Patient","active":false}`), &patient); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(patient)
	if err != nil {
		t.Fatal(err)
	}
	if patient.Active == nil || *patient.Active || !strings.Contains(string(encoded), `"active":false`) {
		t.Fatalf("explicit false was lost: %s", encoded)
	}
	if strings.Contains(string(encoded), `"meta"`) || strings.Contains(string(encoded), `"text"`) {
		t.Fatalf("absent objects were emitted: %s", encoded)
	}
	var observation Observation
	if err := json.Unmarshal([]byte(`{"valueQuantity":{"value":0,"unit":"test-unit"}}`), &observation); err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if observation.ValueQuantity.Value == nil || !strings.Contains(string(encoded), `"value":0`) {
		t.Fatalf("explicit zero was lost: %s", encoded)
	}
	var duration Duration
	if err := json.Unmarshal([]byte(`{"value":0}`), &duration); err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(duration)
	if string(encoded) != `{"value":0}` {
		t.Fatalf("duration zero was lost: %s", encoded)
	}
	encoded, _ = json.Marshal(Timing{Event: []string{"2026-10-26"}})
	if strings.Contains(string(encoded), `"code"`) {
		t.Fatalf("absent timing code was emitted: %s", encoded)
	}
	encoded, _ = json.Marshal(Condition{ResourceType: "Condition", Subject: Reference{Reference: "Patient/p1"}})
	for _, field := range []string{"meta", "clinicalStatus", "verificationStatus", "severity", "encounter", "participant"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Fatalf("absent %s was emitted: %s", field, encoded)
		}
	}
}

func TestPatientValidation(t *testing.T) {
	for _, test := range []struct {
		name, document string
		valid          bool
	}{
		{"minimal base resource", `{"resourceType":"Patient"}`, true},
		{"partial year", `{"resourceType":"Patient","birthDate":"2000"}`, true},
		{"partial month", `{"resourceType":"Patient","birthDate":"2000-02"}`, true},
		{"leap day", `{"resourceType":"Patient","birthDate":"2000-02-29"}`, true},
		{"invalid day", `{"resourceType":"Patient","birthDate":"2001-02-29"}`, false},
		{"invalid year", `{"resourceType":"Patient","birthDate":"0000"}`, false},
		{"invalid gender", `{"resourceType":"Patient","gender":"invalid"}`, false},
		{"wrong type", `{"resourceType":"Observation"}`, false},
		{"missing type", `{}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var p Patient
			if err := json.Unmarshal([]byte(test.document), &p); err != nil {
				t.Fatal(err)
			}
			if err := p.Validate(); (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}

func TestPatientReferences(t *testing.T) {
	for _, reference := range []string{"", "P", "Patient/", "Person/p1", "Patient/p1/extra", "Patient/a_b", "https://example.org/Patient/p1", "Patient/" + strings.Repeat("a", 65)} {
		if _, err := PatientID(reference); !errors.Is(err, ErrInvalid) {
			t.Errorf("reference %q: %v", reference, err)
		}
	}
	for _, value := range []string{"p1", "Patient/p1"} {
		if id, err := PatientSearchID(value); err != nil || id != "p1" {
			t.Errorf("search %q: id=%q err=%v", value, id, err)
		}
	}
}

func TestClinicalValidation(t *testing.T) {
	o := Observation{ResourceType: "Observation", Status: "final", Subject: Reference{Reference: "Patient/p1"}, Code: CodeableConcept{Coding: []Coding{{System: "urn:test", Code: "measurement"}}}}
	if err := o.Validate(); err != nil {
		t.Fatalf("coded-only observation rejected: %v", err)
	}
	o.Status = ""
	if err := o.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing status accepted: %v", err)
	}
	c := Condition{ResourceType: "Condition", Subject: Reference{Reference: "Patient/p1"}, Code: CodeableConcept{Coding: []Coding{{Code: "diagnosis"}}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.OnsetDateTime, c.OnsetString = "2026", "last year"
	if err := c.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("two onset choices accepted: %v", err)
	}
	c.OnsetString = ""
	c.AbatementDateTime = "2026"
	if err := c.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("abatement without clinical status accepted: %v", err)
	}
	c.ClinicalStatus = &CodeableConcept{Coding: []Coding{{System: "http://terminology.hl7.org/CodeSystem/condition-clinical", Code: "resolved"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.VerificationStatus = &CodeableConcept{Coding: []Coding{{System: "http://terminology.hl7.org/CodeSystem/condition-ver-status", Code: "entered-in-error"}}}
	if err := c.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("entered-in-error with clinical status accepted: %v", err)
	}
}

func TestMedicationChoices(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*MedicationRequest)
		valid  bool
	}{
		{"valid concept", func(m *MedicationRequest) {}, true},
		{"valid reference", func(m *MedicationRequest) {
			m.MedicationCodeableConcept = nil
			m.MedicationReference = &Reference{Reference: "Medication/m1"}
		}, true},
		{"missing medication", func(m *MedicationRequest) { m.MedicationCodeableConcept = nil }, false},
		{"both medications", func(m *MedicationRequest) { m.MedicationReference = &Reference{Reference: "Medication/m1"} }, false},
		{"empty concept", func(m *MedicationRequest) { m.MedicationCodeableConcept = &CodeableConcept{} }, false},
		{"missing status", func(m *MedicationRequest) { m.Status = "" }, false},
		{"invalid intent", func(m *MedicationRequest) { m.Intent = "invalid" }, false},
		{"both reported choices", func(m *MedicationRequest) {
			m.ReportedBoolean = new(bool)
			m.ReportedReference = &Reference{Reference: "Patient/p1"}
		}, false},
		{"missing substitution choice", func(m *MedicationRequest) { m.Substitution = &MedicationSubstitution{} }, false},
		{"explicit false substitution", func(m *MedicationRequest) { m.Substitution = &MedicationSubstitution{AllowedBoolean: new(bool)} }, true},
		{"both as-needed choices", func(m *MedicationRequest) {
			m.DosageInstruction = []Dosage{{AsNeededBoolean: new(bool), AsNeededCodeableConcept: &CodeableConcept{Text: "test"}}}
		}, false},
		{"both bounds choices", func(m *MedicationRequest) {
			m.DosageInstruction = []Dosage{{Timing: &Timing{Repeat: &TimingRepeat{BoundsDuration: &Duration{}, BoundsPeriod: &Period{}}}}}
		}, false},
		{"both dose choices", func(m *MedicationRequest) {
			m.DosageInstruction = []Dosage{{DoseAndRate: []DoseAndRate{{DoseRange: &Range{}, DoseQuantity: &Quantity{}}}}}
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := MedicationRequest{ResourceType: "MedicationRequest", Status: "active", Intent: "order", Subject: Reference{Reference: "Patient/p1"}, MedicationCodeableConcept: &CodeableConcept{Text: "test medication"}}
			test.mutate(&m)
			if err := m.Validate(); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
