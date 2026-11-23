package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go-fhir-phase1/internal"
)

// ParseHL7Result parses a clinical HL7 v2.5.1 ORU^R01 unsolicited observation
// message into our internal Result intake representation.
func ParseHL7Result(rawHL7 string) (Result, error) {
	// Normalize line endings to \r (standard HL7 delimiter)
	normalized := strings.ReplaceAll(rawHL7, "\r\n", "\r")
	normalized = strings.ReplaceAll(normalized, "\n", "\r")
	segments := strings.Split(normalized, "\r")

	var result Result
	var msh10, pid3, orc2, obr3, obr7, obx3Code, obx3Name, obx5, obx6, obx11, obx14 string

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if len(seg) < 3 {
			continue
		}
		fields := strings.Split(seg, "|")
		segType := fields[0]

		switch segType {
		case "MSH":
			// In MSH, '|' is MSH-1, so fields[1] is MSH-2, and fields[9] is MSH-10 (Message Control ID).
			if len(fields) > 9 {
				msh10 = fields[9]
			}
		case "PID":
			// PID-3 is Patient Identifier (e.g. "00001^^^SYNTHETIC_LIS^MR" or "00001")
			if len(fields) > 3 {
				pidComponents := strings.Split(fields[3], "^")
				pid3 = pidComponents[0]
			}
		case "ORC":
			// ORC-2 is Placer Order Number
			if len(fields) > 2 {
				orcComponents := strings.Split(fields[2], "^")
				orc2 = orcComponents[0]
			}
		case "OBR":
			// OBR-3 is Filler Order Number / Result ID
			if len(fields) > 3 {
				obrComponents := strings.Split(fields[3], "^")
				obr3 = obrComponents[0]
			}
			// OBR-7 is Observation Date/Time
			if len(fields) > 7 {
				obr7 = fields[7]
			}
		case "OBX":
			// OBX-3 is Observation Identifier (code^text^system)
			if len(fields) > 3 {
				obxComponents := strings.Split(fields[3], "^")
				obx3Code = obxComponents[0]
				if len(obxComponents) > 1 {
					obx3Name = obxComponents[1]
				}
			}
			// OBX-5 is Observation Value
			if len(fields) > 5 {
				obx5 = fields[5]
			}
			// OBX-6 is Units (units^text^system)
			if len(fields) > 6 {
				unitComponents := strings.Split(fields[6], "^")
				obx6 = unitComponents[0]
			}
			// OBX-11 is Observation Result Status (F = Final)
			if len(fields) > 11 {
				obx11 = fields[11]
			}
			// OBX-14 is Date/Time of the Observation
			if len(fields) > 14 && fields[14] != "" {
				obx14 = fields[14]
			}
		}
	}

	if msh10 == "" {
		return Result{}, fmt.Errorf("missing MSH-10 (messageId)")
	}
	if pid3 == "" {
		return Result{}, fmt.Errorf("missing PID-3 (patientIdentifier)")
	}
	if orc2 == "" && obr3 != "" {
		orc2 = obr3
	}
	if orc2 == "" {
		return Result{}, fmt.Errorf("missing ORC-2/OBR-3 (orderIdentifier)")
	}
	if obx3Code == "" {
		return Result{}, fmt.Errorf("missing OBX-3 (test code)")
	}
	if obx5 == "" {
		return Result{}, fmt.Errorf("missing OBX-5 (value)")
	}

	// Status mapping: "F" -> "final"
	status := "final"
	if obx11 != "" && obx11 != "F" {
		status = strings.ToLower(obx11)
	}

	// Effective time parsing: format YYYYMMDDHHMMSS
	effTimeStr := obx14
	if effTimeStr == "" {
		effTimeStr = obr7
	}
	var effectiveTime time.Time
	if effTimeStr != "" {
		if t, err := time.Parse("20060102150405", effTimeStr); err == nil {
			effectiveTime = t
		} else if t, err := time.Parse("200601021504", effTimeStr); err == nil {
			effectiveTime = t
		} else {
			effectiveTime = time.Now().UTC()
		}
	} else {
		effectiveTime = time.Now().UTC()
	}

	num := json.Number(obx5)
	if obx3Name == "" {
		obx3Name = "Laboratory Test " + obx3Code
	}
	if obx6 == "" {
		obx6 = "mmol/L"
	}

	result = Result{
		MessageID: msh10,
		ResultID:  "res-" + msh10,
		PatientIdentifier: internal.Identifier{
			System: "https://example.org/synthetic-lis/patients",
			Value:  pid3,
		},
		OrderIdentifier: internal.Identifier{
			System: "https://example.org/synthetic-lis/orders",
			Value:  orc2,
		},
		Test: internal.Coding{
			System:  "https://example.org/fhir/CodeSystem/lab-tests",
			Code:    obx3Code,
			Display: obx3Name,
		},
		Value: NumericValue{
			Value:  &num,
			Unit:   obx6,
			System: "http://unitsofmeasure.org",
			Code:   obx6,
		},
		Status:            status,
		EffectiveDateTime: effectiveTime.Format(time.RFC3339),
	}

	return result, nil
}
