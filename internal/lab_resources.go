package internal

// These are deliberately selected FHIR R4 v4.0.1 fields, not full resource models.
// Official definitions: https://hl7.org/fhir/R4/servicerequest.html
// https://hl7.org/fhir/R4/specimen.html and https://hl7.org/fhir/R4/diagnosticreport.html
// Models alone do not create endpoints, persist records, or validate profiles.

type ServiceRequest struct {
	ResourceType       string           `json:"resourceType"`
	ID                 string           `json:"id,omitempty"`
	Meta               Meta             `json:"meta,omitzero"`
	Identifier         []Identifier     `json:"identifier,omitempty"`
	Status             string           `json:"status"`             // Required by base R4.
	Intent             string           `json:"intent"`             // Required by base R4.
	Code               *CodeableConcept `json:"code,omitempty"`     // Base R4 permits omission.
	Subject            Reference        `json:"subject"`            // Required; this application's subset uses a local Patient.
	Specimen           []Reference      `json:"specimen,omitempty"` // Array, for specimens already known when ordering.
	OccurrenceDateTime *string          `json:"occurrenceDateTime,omitempty"`
	AuthoredOn         *string          `json:"authoredOn,omitempty"`
}

type Specimen struct {
	ResourceType string       `json:"resourceType"`
	ID           string       `json:"id,omitempty"`
	Meta         Meta         `json:"meta,omitzero"`
	Identifier   []Identifier `json:"identifier,omitempty"`
	// Unlike identifier, accessionIdentifier is ONE object. A pointer omits an
	// absent optional object rather than serializing an empty object.
	AccessionIdentifier *Identifier         `json:"accessionIdentifier,omitempty"`
	Status              *string             `json:"status,omitempty"` // Optional in base R4, even though its value set is required when supplied.
	Type                *CodeableConcept    `json:"type,omitempty"`
	Subject             *Reference          `json:"subject,omitempty"` // Optional in base R4.
	Request             []Reference         `json:"request,omitempty"` // References the order(s); no need to invent an order at receipt.
	ReceivedTime        *string             `json:"receivedTime,omitempty"`
	Collection          *SpecimenCollection `json:"collection,omitempty"`
}

type SpecimenCollection struct {
	// Only this collected[x] alternative is supported here. collectedPeriod
	// remains unsupported, rather than accepting it and silently discarding it.
	CollectedDateTime *string `json:"collectedDateTime,omitempty"`
}

type DiagnosticReport struct {
	ResourceType      string          `json:"resourceType"`
	ID                string          `json:"id,omitempty"`
	Meta              Meta            `json:"meta,omitzero"`
	Identifier        []Identifier    `json:"identifier,omitempty"`
	BasedOn           []Reference     `json:"basedOn,omitempty"`
	Status            string          `json:"status"`            // Required by base R4.
	Code              CodeableConcept `json:"code"`              // Required by base R4.
	Subject           *Reference      `json:"subject,omitempty"` // Base optional; the future lab workflow will require patient context.
	EffectiveDateTime *string         `json:"effectiveDateTime,omitempty"`
	Issued            *string         `json:"issued,omitempty"` // instant: a full timestamp, unlike partial dateTime values.
	// Both are arrays in DiagnosticReport. Observation.specimen is a single reference.
	Specimen []Reference `json:"specimen,omitempty"`
	Result   []Reference `json:"result,omitempty"`
}
