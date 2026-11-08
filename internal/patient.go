package internal

// Gender is a custom type for FHIR gender value sets.
// type Gender int

// const (
// 	GenderUnknown Gender = iota // 0
// 	GenderMale                  // 1
// 	GenderFemale                // 2
// 	GenderOther                 // 3
// )

// // String returns the FHIR-compliant string representation of the gender.
// func (g Gender) String() string {
// 	return [...]string{"unknown", "male", "female", "other"}[g]
// }

// // UnmarshalJSON allows us to accept "male", "female", etc., in JSON.
// func (g *Gender) UnmarshalJSON(data []byte) error {
// 	var s string
// 	if err := json.Unmarshal(data, &s); err != nil {
// 		var i int
// 		if err := json.Unmarshal(data, &i); err != nil {
// 			return fmt.Errorf("invalid gender value")
// 		}
// 		*g = Gender(i)
// 		return nil
// 	}

// 	switch strings.ToLower(s) {
// 	case "male":
// 		*g = GenderMale
// 	case "female":
// 		*g = GenderFemale
// 	case "other":
// 		*g = GenderOther
// 	default:
// 		*g = GenderUnknown
// 	}
// 	return nil
// }

// MarshalJSON ensures that when we send JSON to the client,
// we send "male" instead of 1.
// func (g Gender) MarshalJSON() ([]byte, error) {
// 	return json.Marshal(g.String())
// }

// Meta represents the FHIR metadata for a resource.
type Meta struct {
	VersionID   string `json:"versionId,omitempty"`
	LastUpdated string `json:"lastUpdated,omitempty"`
}

// Extension allows adding custom data that isn't part of the standard FHIR spec.
type Extension struct {
	URL         string  `json:"url"`
	ValueString *string `json:"valueString,omitempty"`
}

// Narrative provides a human-readable summary of the resource.
type Narrative struct {
	Status string `json:"status"`
	Div    string `json:"div"`
}

// Patient represents a FHIR Patient resource.
type Patient struct {
	ResourceType string       `json:"resourceType"`
	ID           string       `json:"id,omitempty"`
	Meta         Meta         `json:"meta,omitzero"`
	Text         *Narrative   `json:"text,omitempty"`
	Extension    []Extension  `json:"extension,omitempty"`
	Identifier   []Identifier `json:"identifier,omitempty"`
	Active       *bool        `json:"active,omitempty"`
	Name         []HumanName  `json:"name,omitempty"`
	Gender       string       `json:"gender,omitempty"`
	BirthDate    string       `json:"birthDate,omitempty"`
	Address      []Address    `json:"address,omitempty"`
}

// Observation represents a measurement or assertion.
type Observation struct {
	ResourceType string            `json:"resourceType"`
	ID           string            `json:"id,omitempty"`
	Meta         Meta              `json:"meta,omitzero"`
	Identifier   []Identifier      `json:"identifier,omitempty"`
	BasedOn      []Reference       `json:"basedOn,omitempty"`
	Status       string            `json:"status"`
	Category     []CodeableConcept `json:"category,omitempty"`
	Code         CodeableConcept   `json:"code"`
	Subject      Reference         `json:"subject"`
	Encounter    *Reference        `json:"encounter,omitempty"`
	// R4 uses one specimen reference here; DiagnosticReport.specimen is an array.
	Specimen          *Reference `json:"specimen,omitempty"`
	EffectiveDateTime string     `json:"effectiveDateTime,omitempty"`
	ValueQuantity     *Quantity  `json:"valueQuantity,omitempty"`
}

// MedicationRequest represents a FHIR R4 medication order or prescription.
type MedicationRequest struct {
	ResourceType              string                     `json:"resourceType"`
	ID                        string                     `json:"id,omitempty"`
	Meta                      Meta                       `json:"meta,omitzero"`
	Text                      *Narrative                 `json:"text,omitempty"`
	Identifier                []Identifier               `json:"identifier,omitempty"`
	Status                    string                     `json:"status"`
	StatusReason              *CodeableConcept           `json:"statusReason,omitempty"`
	Intent                    string                     `json:"intent"`
	Category                  []CodeableConcept          `json:"category,omitempty"`
	Priority                  string                     `json:"priority,omitempty"`
	DoNotPerform              *bool                      `json:"doNotPerform,omitempty"`
	ReportedBoolean           *bool                      `json:"reportedBoolean,omitempty"`
	ReportedReference         *Reference                 `json:"reportedReference,omitempty"`
	MedicationCodeableConcept *CodeableConcept           `json:"medicationCodeableConcept,omitempty"`
	MedicationReference       *Reference                 `json:"medicationReference,omitempty"`
	Subject                   Reference                  `json:"subject"`
	Encounter                 *Reference                 `json:"encounter,omitempty"`
	SupportingInformation     []Reference                `json:"supportingInformation,omitempty"`
	AuthoredOn                string                     `json:"authoredOn,omitempty"`
	Requester                 *Reference                 `json:"requester,omitempty"`
	Performer                 *Reference                 `json:"performer,omitempty"`
	PerformerType             *CodeableConcept           `json:"performerType,omitempty"`
	Recorder                  *Reference                 `json:"recorder,omitempty"`
	ReasonCode                []CodeableConcept          `json:"reasonCode,omitempty"`
	ReasonReference           []Reference                `json:"reasonReference,omitempty"`
	InstantiatesCanonical     []string                   `json:"instantiatesCanonical,omitempty"`
	InstantiatesURI           []string                   `json:"instantiatesUri,omitempty"`
	BasedOn                   []Reference                `json:"basedOn,omitempty"`
	GroupIdentifier           *Identifier                `json:"groupIdentifier,omitempty"`
	CourseOfTherapyType       *CodeableConcept           `json:"courseOfTherapyType,omitempty"`
	Insurance                 []Reference                `json:"insurance,omitempty"`
	Note                      []Annotation               `json:"note,omitempty"`
	DosageInstruction         []Dosage                   `json:"dosageInstruction,omitempty"`
	DispenseRequest           *MedicationDispenseRequest `json:"dispenseRequest,omitempty"`
	Substitution              *MedicationSubstitution    `json:"substitution,omitempty"`
	PriorPrescription         *Reference                 `json:"priorPrescription,omitempty"`
	DetectedIssue             []Reference                `json:"detectedIssue,omitempty"`
	EventHistory              []Reference                `json:"eventHistory,omitempty"`
}

type CodeableConcept struct {
	Coding []Coding `json:"coding,omitempty"`
	Text   string   `json:"text,omitempty"`
}

type Coding struct {
	System  string `json:"system,omitempty"`
	Code    string `json:"code,omitempty"`
	Display string `json:"display,omitempty"`
}

type Quantity struct {
	Value  *float64 `json:"value,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	System string   `json:"system,omitempty"`
	Code   string   `json:"code,omitempty"`
}

type Duration struct {
	Value  *float64 `json:"value,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	System string   `json:"system,omitempty"`
	Code   string   `json:"code,omitempty"`
}

type Period struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type Timing struct {
	Event  []string         `json:"event,omitempty"`
	Repeat *TimingRepeat    `json:"repeat,omitempty"`
	Code   *CodeableConcept `json:"code,omitempty"`
}

type TimingRepeat struct {
	BoundsDuration *Duration `json:"boundsDuration,omitempty"`
	BoundsRange    *Range    `json:"boundsRange,omitempty"`
	BoundsPeriod   *Period   `json:"boundsPeriod,omitempty"`
	Count          *int      `json:"count,omitempty"`
	CountMax       *int      `json:"countMax,omitempty"`
	Duration       *float64  `json:"duration,omitempty"`
	DurationMax    *float64  `json:"durationMax,omitempty"`
	DurationUnit   string    `json:"durationUnit,omitempty"`
	Frequency      *int      `json:"frequency,omitempty"`
	FrequencyMax   *int      `json:"frequencyMax,omitempty"`
	Period         *float64  `json:"period,omitempty"`
	PeriodMax      *float64  `json:"periodMax,omitempty"`
	PeriodUnit     string    `json:"periodUnit,omitempty"`
	When           []string  `json:"when,omitempty"`
	Offset         *int      `json:"offset,omitempty"`
}

type Range struct {
	Low  *Quantity `json:"low,omitempty"`
	High *Quantity `json:"high,omitempty"`
}

type Ratio struct {
	Numerator   *Quantity `json:"numerator,omitempty"`
	Denominator *Quantity `json:"denominator,omitempty"`
}

type Dosage struct {
	Sequence                 *int              `json:"sequence,omitempty"`
	Text                     string            `json:"text,omitempty"`
	AdditionalInstruction    []CodeableConcept `json:"additionalInstruction,omitempty"`
	PatientInstruction       string            `json:"patientInstruction,omitempty"`
	Timing                   *Timing           `json:"timing,omitempty"`
	AsNeededBoolean          *bool             `json:"asNeededBoolean,omitempty"`
	AsNeededCodeableConcept  *CodeableConcept  `json:"asNeededCodeableConcept,omitempty"`
	Site                     *CodeableConcept  `json:"site,omitempty"`
	Route                    *CodeableConcept  `json:"route,omitempty"`
	Method                   *CodeableConcept  `json:"method,omitempty"`
	DoseAndRate              []DoseAndRate     `json:"doseAndRate,omitempty"`
	MaxDosePerPeriod         *Ratio            `json:"maxDosePerPeriod,omitempty"`
	MaxDosePerAdministration *Quantity         `json:"maxDosePerAdministration,omitempty"`
	MaxDosePerLifetime       *Quantity         `json:"maxDosePerLifetime,omitempty"`
}

type DoseAndRate struct {
	Type         *CodeableConcept `json:"type,omitempty"`
	DoseRange    *Range           `json:"doseRange,omitempty"`
	DoseQuantity *Quantity        `json:"doseQuantity,omitempty"`
	RateRatio    *Ratio           `json:"rateRatio,omitempty"`
	RateRange    *Range           `json:"rateRange,omitempty"`
	RateQuantity *Quantity        `json:"rateQuantity,omitempty"`
}

type MedicationDispenseRequest struct {
	InitialFill            *MedicationInitialFill `json:"initialFill,omitempty"`
	DispenseInterval       *Duration              `json:"dispenseInterval,omitempty"`
	ValidityPeriod         *Period                `json:"validityPeriod,omitempty"`
	NumberOfRepeatsAllowed *int                   `json:"numberOfRepeatsAllowed,omitempty"`
	Quantity               *Quantity              `json:"quantity,omitempty"`
	ExpectedSupplyDuration *Duration              `json:"expectedSupplyDuration,omitempty"`
	Performer              *Reference             `json:"performer,omitempty"`
}

type MedicationInitialFill struct {
	Quantity *Quantity `json:"quantity,omitempty"`
	Duration *Duration `json:"duration,omitempty"`
}

type MedicationSubstitution struct {
	AllowedBoolean         *bool            `json:"allowedBoolean,omitempty"`
	AllowedCodeableConcept *CodeableConcept `json:"allowedCodeableConcept,omitempty"`
	Reason                 *CodeableConcept `json:"reason,omitempty"`
}

type Reference struct {
	Reference string `json:"reference,omitempty"`
}

type Identifier struct {
	System string `json:"system,omitempty"`
	Value  string `json:"value,omitempty"`
}

type HumanName struct {
	Family string   `json:"family,omitempty"`
	Given  []string `json:"given,omitempty"`
	Use    string   `json:"use,omitempty"`
}

type Address struct {
	Line       []string `json:"line,omitempty"`
	City       string   `json:"city,omitempty"`
	District   string   `json:"district,omitempty"`
	State      string   `json:"state,omitempty"`
	PostalCode string   `json:"postalCode,omitempty"`
	Country    string   `json:"country,omitempty"`
}

type Bundle struct {
	ResourceType string        `json:"resourceType"`
	Type         string        `json:"type"`
	Total        *int          `json:"total,omitempty"`
	Entry        []BundleEntry `json:"entry,omitempty"`
}

type BundleEntry struct {
	FullUrl  string          `json:"fullUrl,omitempty"`
	Resource interface{}     `json:"resource,omitempty"`
	Request  *BundleRequest  `json:"request,omitempty"`
	Response *BundleResponse `json:"response,omitempty"`
}

type BundleRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type BundleResponse struct {
	Status       string `json:"status"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

// Condition represents a FHIR Condition resource (Diagnoses).
type Condition struct {
	ResourceType       string              `json:"resourceType"`
	ID                 string              `json:"id,omitempty"`
	Meta               Meta                `json:"meta,omitzero"`
	Text               *Narrative          `json:"text,omitempty"`
	Identifier         []Identifier        `json:"identifier,omitempty"`
	ClinicalStatus     *CodeableConcept    `json:"clinicalStatus,omitempty"`
	VerificationStatus *CodeableConcept    `json:"verificationStatus,omitempty"`
	Category           []CodeableConcept   `json:"category,omitempty"`
	Severity           *CodeableConcept    `json:"severity,omitempty"`
	Code               CodeableConcept     `json:"code"`
	BodySite           []CodeableConcept   `json:"bodySite,omitempty"`
	Subject            Reference           `json:"subject"`
	Encounter          *Reference          `json:"encounter,omitempty"`
	OnsetDateTime      string              `json:"onsetDateTime,omitempty"`
	OnsetAge           *Quantity           `json:"onsetAge,omitempty"`
	OnsetPeriod        *Period             `json:"onsetPeriod,omitempty"`
	OnsetRange         *Range              `json:"onsetRange,omitempty"`
	OnsetString        string              `json:"onsetString,omitempty"`
	AbatementDateTime  string              `json:"abatementDateTime,omitempty"`
	AbatementAge       *Quantity           `json:"abatementAge,omitempty"`
	AbatementPeriod    *Period             `json:"abatementPeriod,omitempty"`
	AbatementRange     *Range              `json:"abatementRange,omitempty"`
	AbatementString    string              `json:"abatementString,omitempty"`
	RecordedDate       string              `json:"recordedDate,omitempty"`
	Recorder           *Reference          `json:"recorder,omitempty"`
	Asserter           *Reference          `json:"asserter,omitempty"`
	Stage              []ConditionStage    `json:"stage,omitempty"`
	Evidence           []ConditionEvidence `json:"evidence,omitempty"`
	Note               []Annotation        `json:"note,omitempty"`
}

type ConditionEvidence struct {
	Code   []CodeableConcept `json:"code,omitempty"`
	Detail []Reference       `json:"detail,omitempty"`
}

type ConditionStage struct {
	Summary    *CodeableConcept `json:"summary,omitempty"`
	Assessment []Reference      `json:"assessment,omitempty"`
	Type       *CodeableConcept `json:"type,omitempty"`
}

type Annotation struct {
	Text string `json:"text"`
}

type OperationOutcome struct {
	ResourceType string  `json:"resourceType"`
	Issue        []Issue `json:"issue"`
}

type Issue struct {
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Diagnostics string `json:"diagnostics,omitempty"`
}
