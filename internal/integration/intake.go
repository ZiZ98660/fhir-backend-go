// Package integration coordinates exchange work separately from FHIR CRUD.
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"go-fhir-phase1/internal"
)

// Scope is assigned by server configuration, never by the JSON request body.
// One shared token/one configured source is a synthetic-demo boundary, not RBAC.
type Scope struct{ TenantID, SourceID string }

// Result is our intake contract, NOT a FHIR resource. Identifiers keep their
// namespace and leading zeros; matching never uses a name or approximate text.
type Result struct {
	MessageID         string              `json:"messageId"`
	ResultID          string              `json:"resultId"`
	PatientIdentifier internal.Identifier `json:"patientIdentifier"`
	OrderIdentifier   internal.Identifier `json:"orderIdentifier"`
	Test              internal.Coding     `json:"test"`
	Value             NumericValue        `json:"value"`
	Status            string              `json:"status"`
	EffectiveDateTime string              `json:"effectiveDateTime"`
}

type NumericValue struct {
	// A pointer distinguishes an absent value from an explicit zero. json.Number
	// preserves the source decimal instead of rounding it through float64.
	Value  *json.Number `json:"value"`
	Unit   string       `json:"unit"`
	System string       `json:"system"`
	Code   string       `json:"code"`
}

// A declared demo precision cap keeps accepted numbers inside PostgreSQL's
// JSONB numeric domain. Scientific notation is unsupported in this first slice.
var numericPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,17})(\.[0-9]{1,12})?$`)

type Receipt struct {
	ID         string    `json:"id"`
	State      string    `json:"state"`
	Reason     string    `json:"reason,omitempty"`
	ReceivedAt time.Time `json:"receivedAt"`
}

type Submission struct {
	Result            Result
	RawPayload        []byte
	NormalizedPayload []byte
	ContentHash       string
}

// Store is intentionally small: it describes what this workflow needs, not
// every method that PostgreSQL can provide. Tests can implement the same methods.
type Store interface {
	Receive(context.Context, Scope, Submission) (Receipt, bool, error)
	Get(context.Context, Scope, string) (Receipt, error)
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) Submit(ctx context.Context, scope Scope, raw []byte) (Receipt, bool, error) {
	if !internal.ValidID(scope.TenantID) || !internal.ValidID(scope.SourceID) {
		return Receipt{}, false, internal.Invalid("invalid configured integration scope")
	}
	result, err := DecodeResult(raw)
	if err != nil {
		return Receipt{}, false, err
	}
	// Hash normalized typed content, so whitespace/key order changes alone are
	// retransmissions. Different decimal spellings remain distinct by design;
	// ambiguous changes are rejected rather than silently treated as equivalent.
	normalized, err := json.Marshal(result)
	if err != nil {
		return Receipt{}, false, err
	}
	return s.store.Receive(ctx, scope, Submission{
		Result: result, RawPayload: raw, NormalizedPayload: normalized,
		ContentHash: fmt.Sprintf("%x", sha256.Sum256(normalized)),
	})
}

func (s *Service) Get(ctx context.Context, scope Scope, id string) (Receipt, error) {
	if !internal.ValidID(id) || !internal.ValidID(scope.TenantID) || !internal.ValidID(scope.SourceID) {
		return Receipt{}, internal.Invalid("invalid integration receipt or scope")
	}
	return s.store.Get(ctx, scope, id)
}

func DecodeResult(raw []byte) (Result, error) {
	if !utf8.Valid(raw) {
		return Result{}, internal.Invalid("input must be valid UTF-8 JSON")
	}
	// Go normally accepts repeated object keys, keeping/merging later values.
	// Clinical identity must not depend on that rule: reject ambiguous objects.
	if err := uniqueObjectKeys(raw); err != nil {
		return Result{}, internal.Invalid("invalid or ambiguous JSON object")
	}
	var result *Result
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result == nil {
		// Do not echo parser errors: they can contain supplied clinical values.
		return Result{}, internal.Invalid("expected a supported laboratory result object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Result{}, internal.Invalid("exactly one result object is required")
	}
	if !internal.ValidID(result.MessageID) || !internal.ValidID(result.ResultID) {
		return Result{}, internal.Invalid("messageId and resultId must be valid local ids")
	}
	// PostgreSQL JSONB cannot store NUL characters. Go can also replace escaped
	// unpaired Unicode surrogates with U+FFFD; reject replacement characters here
	// rather than allowing distinct damaged identifiers to normalize to one value.
	for _, value := range []string{
		result.PatientIdentifier.System, result.PatientIdentifier.Value,
		result.OrderIdentifier.System, result.OrderIdentifier.Value, result.Test.Display,
	} {
		if strings.ContainsRune(value, '\x00') || strings.ContainsRune(value, '\uFFFD') {
			return Result{}, internal.Invalid("NUL and Unicode replacement characters are not supported")
		}
	}
	for _, identifier := range []internal.Identifier{result.PatientIdentifier, result.OrderIdentifier} {
		if strings.TrimSpace(identifier.System) == "" || strings.TrimSpace(identifier.Value) == "" {
			return Result{}, internal.Invalid("patient and order identifiers require system and value")
		}
	}
	// First contract: one synthetic numeric glucose scenario, with no implicit
	// terminology mapping, unit conversion, preliminary results, or corrections.
	if result.Test.System != "https://example.org/fhir/CodeSystem/lab-tests" || result.Test.Code != "SYN-GLU" {
		return Result{}, internal.Invalid("test is outside the supported synthetic scenario")
	}
	if result.Value.Value == nil || result.Value.Value.String() == "" {
		return Result{}, internal.Invalid("a numeric result value is required")
	}
	// encoding/json permits quoted strings when decoding into json.Number.
	// Inspect the original token so "5.2" is rejected while numeric 5.2 is kept.
	var tokens struct {
		Value struct {
			Value json.RawMessage `json:"value"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return Result{}, internal.Invalid("invalid numeric result")
	}
	if len(tokens.Value.Value) == 0 || tokens.Value.Value[0] == '"' {
		return Result{}, internal.Invalid("result value must be a JSON number")
	}
	if !numericPattern.MatchString(result.Value.Value.String()) {
		return Result{}, internal.Invalid("numeric results require plain decimals with at most 18 integer and 12 fractional digits")
	}
	if result.Value.System != "http://unitsofmeasure.org" || result.Value.Code != "mmol/L" || result.Value.Unit != "mmol/L" {
		return Result{}, internal.Invalid("the synthetic scenario requires explicit UCUM mmol/L units")
	}
	if result.Status != "final" {
		return Result{}, internal.Invalid("only final results are supported; corrections require reconciliation")
	}
	if !internal.ValidLabInstant(result.EffectiveDateTime) {
		return Result{}, internal.Invalid("effectiveDateTime requires a timestamp with timezone")
	}
	at, _ := time.Parse(time.RFC3339Nano, result.EffectiveDateTime) // Already checked without precision loss.
	result.EffectiveDateTime = at.UTC().Format(time.RFC3339Nano)
	return *result, nil
}

// Walk JSON tokens independently of the typed decode. Each object gets its own
// key set; arrays recurse into their values. UseNumber avoids rounding decimals
// even during this preliminary check. The HTTP boundary limits input to 1 MiB.
func uniqueObjectKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, container := token.(json.Delim)
		if !container {
			return nil
		}
		if delim == '{' {
			seen := make(map[string]bool)
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				// Case-fold as well: encoding/json matches Go fields ignoring case.
				name := strings.ToLower(key.(string))
				if seen[name] {
					return fmt.Errorf("duplicate field")
				}
				seen[name] = true
				if err := visit(); err != nil {
					return err
				}
			}
		} else if delim == '[' {
			for decoder.More() {
				if err := visit(); err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("unexpected delimiter")
		}
		_, err = decoder.Token() // Consume the closing delimiter validated by Decoder.
		return err
	}
	return visit()
}
