package internal

import (
	"regexp"
	"strings"
	"time"
)

// These checks cover the modeled subset, not terminology bindings, FHIRPath
// invariants, profile validation, or the existence/ownership of linked records.
// ServiceRequest requires status, intent and subject in base R4. Its code is
// optional in the base model; a future laboratory workflow can require it locally.
func (s ServiceRequest) Validate() error {
	if err := resourceType(s.ResourceType, "ServiceRequest"); err != nil {
		return err
	}
	if err := labIdentifiers(s.Identifier); err != nil {
		return err
	}
	if !oneOf(s.Status, "draft", "active", "on-hold", "revoked", "completed", "entered-in-error", "unknown") {
		return Invalid("invalid or missing ServiceRequest.status")
	}
	if !oneOf(s.Intent, "proposal", "plan", "directive", "order", "original-order", "reflex-order", "filler-order", "instance-order", "option") {
		return Invalid("invalid or missing ServiceRequest.intent")
	}
	if _, err := PatientID(s.Subject.Reference); err != nil {
		return err
	}
	if s.Code != nil && !s.Code.HasValue() {
		return Invalid("supplied ServiceRequest.code must contain a code or text")
	}
	for _, ref := range s.Specimen {
		if err := labReference(ref, "Specimen"); err != nil {
			return err
		}
	}
	for _, value := range []*string{s.OccurrenceDateTime, s.AuthoredOn} {
		if value != nil && !labDateTime(*value) {
			return Invalid("invalid supported ServiceRequest dateTime")
		}
	}
	return nil
}

// Specimen has no resource-specific mandatory base fields. Check optional values
// only when supplied, rather than inventing a status or patient association.
func (s Specimen) Validate() error {
	if err := resourceType(s.ResourceType, "Specimen"); err != nil {
		return err
	}
	if err := labIdentifiers(s.Identifier); err != nil {
		return err
	}
	if s.Status != nil && !oneOf(*s.Status, "available", "unavailable", "unsatisfactory", "entered-in-error") {
		return Invalid("invalid Specimen.status")
	}
	if s.Subject != nil {
		if _, err := PatientID(s.Subject.Reference); err != nil {
			return err
		}
	}
	if s.Type != nil && !s.Type.HasValue() {
		return Invalid("supplied Specimen.type must contain a code or text")
	}
	for _, ref := range s.Request {
		if err := labReference(ref, "ServiceRequest"); err != nil {
			return err
		}
	}
	if s.ReceivedTime != nil && !labDateTime(*s.ReceivedTime) {
		return Invalid("invalid Specimen.receivedTime")
	}
	if s.Collection != nil {
		if s.Collection.CollectedDateTime == nil || !labDateTime(*s.Collection.CollectedDateTime) {
			return Invalid("supported Specimen.collection requires collectedDateTime")
		}
	}
	if s.AccessionIdentifier != nil && s.AccessionIdentifier.System == "" && s.AccessionIdentifier.Value == "" {
		return Invalid("accessionIdentifier cannot be empty")
	}
	return nil
}

// A report requires status and code. Its numeric measurements live in referenced
// Observations, while these local link checks only validate reference syntax.
func (d DiagnosticReport) Validate() error {
	if err := resourceType(d.ResourceType, "DiagnosticReport"); err != nil {
		return err
	}
	if err := labIdentifiers(d.Identifier); err != nil {
		return err
	}
	if !oneOf(d.Status, "registered", "partial", "preliminary", "final", "amended", "corrected", "appended", "cancelled", "entered-in-error", "unknown") {
		return Invalid("invalid or missing DiagnosticReport.status")
	}
	if !d.Code.HasValue() {
		return Invalid("DiagnosticReport requires code.text or code.coding.code")
	}
	if d.Subject != nil {
		if _, err := PatientID(d.Subject.Reference); err != nil {
			return err
		}
	}
	for _, ref := range d.BasedOn {
		if err := labReference(ref, "ServiceRequest"); err != nil {
			return err
		}
	}
	for _, ref := range d.Specimen {
		if err := labReference(ref, "Specimen"); err != nil {
			return err
		}
	}
	for _, ref := range d.Result {
		if err := labReference(ref, "Observation"); err != nil {
			return err
		}
	}
	if d.EffectiveDateTime != nil && !labDateTime(*d.EffectiveDateTime) {
		return Invalid("invalid DiagnosticReport.effectiveDateTime")
	}
	// issued is an instant, so a year/month/date alone must not pass here.
	if d.Issued != nil {
		if !ValidLabInstant(*d.Issued) {
			return Invalid("DiagnosticReport.issued requires a timestamp with timezone")
		}
	}
	return nil
}

// This application supports local references of a specified kind. Syntax checks
// do not prove the target exists; the later workflow must check that separately.
func labReference(ref Reference, kind string) error {
	id, ok := strings.CutPrefix(ref.Reference, kind+"/")
	if !ok || !ValidID(id) {
		return Invalid("expected a local %s reference", kind)
	}
	return nil
}

// FHIR dateTime can be a partial calendar date. When a time is supplied this
// subset requires seconds and an explicit timezone. No missing time is invented.
func labDateTime(value string) bool {
	for _, layout := range []string{"2006", "2006-01", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil && parsed.Year() > 0 {
			return true
		}
	}
	return ValidLabInstant(value)
}

func labIdentifiers(values []Identifier) error {
	for _, value := range values {
		if strings.TrimSpace(value.System) == "" && strings.TrimSpace(value.Value) == "" {
			return Invalid("identifier entries cannot be empty")
		}
	}
	return nil
}

// Go's RFC3339 parser accepts some non-FHIR spellings and truncates fractions
// beyond nanoseconds. Restrict the lexical shape BEFORE parsing, so validation
// and hashing never silently lose precision. Leap seconds and >9 fractional
// digits remain unsupported in this subset, even though base R4 permits them.
// https://hl7.org/fhir/R4/datatypes.html#instant
var labInstantPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]((0[0-9]|1[0-3]):[0-5][0-9]|14:00))$`)

func ValidLabInstant(value string) bool {
	if !labInstantPattern.MatchString(value) {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	// Calendar parsing catches impossible dates; UTC bounds keep normalization
	// from turning a supported positive year into year zero or a five-digit year.
	return err == nil && parsed.Year() > 0 && parsed.UTC().Year() > 0 && parsed.UTC().Year() <= 9999
}
