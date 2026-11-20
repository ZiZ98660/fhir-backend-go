package internal_test

import (
	"context"
	"testing"

	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/service"
)

type mockPatientStore struct{}

func (m *mockPatientStore) InitTable(ctx context.Context) error                         { return nil }
func (m *mockPatientStore) SavePatient(ctx context.Context, p internal.Patient, data []byte) error {
	return nil
}
func (m *mockPatientStore) GetPatients(ctx context.Context) ([][]byte, error) { return nil, nil }
func (m *mockPatientStore) GetPatientByID(ctx context.Context, id string) ([]byte, error) {
	return nil, nil
}

func TestPatientValidation(t *testing.T) {
	svc := service.NewPatientService(&mockPatientStore{})

	// Test missing family name
	_, err := svc.CreatePatient(context.Background(), internal.Patient{
		ResourceType: "Patient",
		Name:         []internal.HumanName{{Given: []string{"John"}}},
	})
	if err == nil {
		t.Fatalf("expected error when family name is missing")
	}

	// Test valid patient
	p, err := svc.CreatePatient(context.Background(), internal.Patient{
		ResourceType: "Patient",
		Name:         []internal.HumanName{{Family: "Doe", Given: []string{"John"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error creating valid patient: %v", err)
	}
	if p.ID == "" {
		t.Fatalf("expected generated patient ID")
	}
}
