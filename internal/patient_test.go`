package internal

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestPatientTransformation(t *testing.T) {
	rawJson := []byte(`{
		"id" : "pt-003",
		"name" : "Sadiq",
		"address" : {
			"city" : "Ikeja",
			"state" : "Lagos"
		}
	}`)

	// Use the Patient struct from the same package
	var namedPatient Patient

	err := json.Unmarshal(rawJson, &namedPatient)
	if err != nil {
		t.Fatalf("Error parsing JSON: %v\n", err)
	}

	namedPatient.GivenNames[0] = "Alex"
	if namedPatient.Address != nil {
		namedPatient.Address.City = "Surulere"
	}

	if namedPatient.GivenNames[0] != "Alex" {
		t.Errorf("Expected name Alex, got %v", namedPatient.GivenNames[0])
	}

	if namedPatient.Address != nil && namedPatient.Address.City != "Surulere" {
		t.Errorf("Expected city Surulere, got %s", namedPatient.Address.City)
	}

	jsonB, err := json.MarshalIndent(namedPatient, "", "  ")
	if err != nil {
		t.Errorf("Error Generating JSON: %v\n", err)
	}

	fmt.Println(string(jsonB))
}
