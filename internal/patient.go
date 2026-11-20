package internal

// Patient represents a FHIR Patient resource.
// Official FHIR resources use slices for names and identifiers because
// a person can have multiple legal names or official IDs.
type Patient struct {
	ResourceType string       `json:"resourceType"`
	ID           string       `json:"id,omitempty"`
	Identifier   []Identifier `json:"identifier,omitempty"`
	Active       bool         `json:"active"`
	Name         []HumanName  `json:"name,omitempty"`
	Gender       string       `json:"gender,omitempty"`
	BirthDate    string       `json:"birthDate,omitempty"`
	Address      []Address    `json:"address,omitempty"`
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

// Bundle is a FHIR container for a collection of resources.
// When you perform a search, FHIR doesn't return a simple array;
// it returns a Bundle of type "search".
type Bundle struct {
	ResourceType string        `json:"resourceType"`
	Type         string        `json:"type"` // e.g., "search"
	Total        int           `json:"total,omitempty"`
	Entry        []BundleEntry `json:"entry,omitempty"`
}

type BundleEntry struct {
	FullUrl  string      `json:"fullUrl,omitempty"`
	Resource interface{} `json:"resource,omitempty"`
}
