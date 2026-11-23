package integration

import (
	"testing"
)

func TestParseHL7Result(t *testing.T) {
	sampleHL7 := "MSH|^~\\&|COBAS_C311|HOSP_LAB|CORE_EMR|MAIN_CAMPUS|20261123103000||ORU^R01|MSG-HL7-001|P|2.5.1\r" +
		"PID|1||00001^^^SYNTHETIC_LIS^MR||Smith^John^^^^^L||19850412|M\r" +
		"ORC|RE|SYN-ORDER-1^LIS|ORD-98765^EMR||CM\r" +
		"OBR|1|SYN-ORDER-1^LIS|ORD-98765^EMR|GLU^Glucose, Fasting^LN|||20261123102000\r" +
		"OBX|1|NM|SYN-GLU^Synthetic numeric glucose scenario^SYN_LAB|1|5.20|mmol/L^mmol/L^UCUM|3.9-6.1|N|||F|||20261123102000"

	res, err := ParseHL7Result(sampleHL7)
	if err != nil {
		t.Fatalf("unexpected error parsing HL7: %v", err)
	}

	if res.MessageID != "MSG-HL7-001" {
		t.Errorf("expected MessageID MSG-HL7-001, got %s", res.MessageID)
	}
	if res.PatientIdentifier.Value != "00001" {
		t.Errorf("expected PatientIdentifier 00001, got %s", res.PatientIdentifier.Value)
	}
	if res.OrderIdentifier.Value != "SYN-ORDER-1" {
		t.Errorf("expected OrderIdentifier SYN-ORDER-1, got %s", res.OrderIdentifier.Value)
	}
	if res.Test.Code != "SYN-GLU" {
		t.Errorf("expected Test.Code SYN-GLU, got %s", res.Test.Code)
	}
	if res.Value.Value == nil || string(*res.Value.Value) != "5.20" {
		t.Errorf("expected Value 5.20, got %v", res.Value.Value)
	}
	if res.Status != "final" {
		t.Errorf("expected status final, got %s", res.Status)
	}
}
