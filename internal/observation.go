package internal

// Observation represents a measurement or assertion.
type Observation struct {
	ResourceType      string            `json:"resourceType"`
	ID                string            `json:"id,omitempty"`
	Meta              Meta              `json:"meta,omitzero"`
	Identifier        []Identifier      `json:"identifier,omitempty"`
	BasedOn           []Reference       `json:"basedOn,omitempty"`
	Status            string            `json:"status"`
	Category          []CodeableConcept `json:"category,omitempty"`
	Code              CodeableConcept   `json:"code"`
	Subject           Reference         `json:"subject"`
	Encounter         *Reference        `json:"encounter,omitempty"`
	// R4 uses one specimen reference here; DiagnosticReport.specimen is an array.
	Specimen          *Reference        `json:"specimen,omitempty"`
	EffectiveDateTime string            `json:"effectiveDateTime,omitempty"`
	ValueQuantity     *Quantity         `json:"valueQuantity,omitempty"`
}
