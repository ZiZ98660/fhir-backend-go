package internal

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
