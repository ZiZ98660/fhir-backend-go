package internal

import "encoding/json"

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
	// json.Number keeps clinical decimal precision, including meaningful zeros.
	Value  *json.Number `json:"value,omitempty"`
	Unit   string       `json:"unit,omitempty"`
	System string       `json:"system,omitempty"`
	Code   string       `json:"code,omitempty"`
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

type Reference struct {
	Reference string `json:"reference,omitempty"`
}

type Annotation struct {
	Text string `json:"text"`
}

type Bundle struct {
	ResourceType string        `json:"resourceType"`
	Type         string        `json:"type"`
	Total        *int          `json:"total,omitempty"`
	Entry        []BundleEntry `json:"entry,omitempty"`
}

type BundleEntry struct {
	FullUrl  string          `json:"fullUrl,omitempty"`
	Resource any             `json:"resource,omitempty"`
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

type OperationOutcome struct {
	ResourceType string  `json:"resourceType"`
	Issue        []Issue `json:"issue"`
}

type Issue struct {
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Diagnostics string `json:"diagnostics,omitempty"`
}
