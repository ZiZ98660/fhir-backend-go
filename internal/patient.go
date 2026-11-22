package internal

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
