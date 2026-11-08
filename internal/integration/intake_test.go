package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"go-fhir-phase1/internal"
)

func sample(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/integration/synthetic-result.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type captureStore struct {
	called  bool
	scope   Scope
	message Submission
}

func (s *captureStore) Receive(_ context.Context, scope Scope, message Submission) (Receipt, bool, error) {
	s.called, s.scope, s.message = true, scope, message
	return Receipt{ID: "msg-test", State: "needs_reconciliation"}, false, nil
}
func (s *captureStore) Get(context.Context, Scope, string) (Receipt, error) {
	return Receipt{}, internal.ErrNotFound
}

func TestIntakePreservesIdentifiersDecimalsAndRawInput(t *testing.T) {
	raw := sample(t)
	store := &captureStore{}
	scope := Scope{"synthetic-demo", "synthetic-lis"}
	_, _, err := NewService(store).Submit(context.Background(), scope, raw)
	if err != nil {
		t.Fatal(err)
	}
	if store.scope != scope || !bytes.Equal(store.message.RawPayload, raw) {
		t.Fatal("scope or original input changed")
	}
	result := store.message.Result
	if result.PatientIdentifier.Value != "00001" || result.Value.Value.String() != "5.20" {
		t.Fatal("identifier or decimal precision was lost")
	}
	if result.EffectiveDateTime != "2026-11-08T08:00:00Z" {
		t.Fatal("timestamp not normalized to UTC")
	}
	if !strings.Contains(string(store.message.NormalizedPayload), `"value":5.20`) {
		t.Fatal("numeric result was rounded or quoted")
	}
	firstHash := store.message.ContentHash
	// JSON whitespace is transport formatting, so it must not change identity.
	if _, _, err := NewService(store).Submit(context.Background(), scope, bytes.TrimSpace(raw)); err != nil {
		t.Fatal(err)
	}
	if firstHash != store.message.ContentHash {
		t.Fatal("formatting changed the content hash")
	}
	zero := bytes.Replace(raw, []byte("5.20"), []byte("0"), 1)
	if _, err := DecodeResult(zero); err != nil {
		t.Fatalf("explicit zero rejected: %v", err)
	}
}

func TestInvalidIntakeNeverReachesStorage(t *testing.T) {
	raw := string(sample(t))
	tests := map[string]string{
		"null": "null", "array": "[]", "multiple": raw + raw,
		"duplicate key":              strings.Replace(raw, `"messageId":`, `"messageId":"other", "messageId":`, 1),
		"case duplicate key":         strings.Replace(raw, `"messageId":`, `"MessageId":"other", "messageId":`, 1),
		"nested duplicate key":       strings.Replace(raw, `"value": 5.20`, `"value": 1, "value": 5.20`, 1),
		"unknown":                    strings.Replace(raw, `"messageId":`, `"tenantId":"spoofed", "messageId":`, 1),
		"missing patient namespace":  strings.Replace(raw, `"https://example.org/synthetic-lis/patients"`, `""`, 1),
		"missing order":              strings.Replace(raw, `"SYN-ORDER-1"`, `""`, 1),
		"missing number":             strings.Replace(raw, "5.20", "null", 1),
		"quoted number":              strings.Replace(raw, "5.20", `"5.20"`, 1),
		"numeric overflow":           strings.Replace(raw, "5.20", "1e1000000", 1),
		"too much numeric precision": strings.Replace(raw, "5.20", "5.1234567890123", 1),
		"NUL identifier":             strings.Replace(raw, `"00001"`, `"00\u000001"`, 1),
		"damaged unicode identifier": strings.Replace(raw, `"00001"`, `"\ud800"`, 1),
		"unit conversion":            strings.ReplaceAll(raw, "mmol/L", "mg/dL"),
		"unknown test":               strings.Replace(raw, "SYN-GLU", "UNKNOWN", 1),
		"correction":                 strings.Replace(raw, `"final"`, `"corrected"`, 1),
		"timezone missing":           strings.Replace(raw, "2026-11-08T09:00:00+01:00", "2026-11-08T09:00:00", 1),
		"timestamp precision loss":   strings.Replace(raw, "2026-11-08T09:00:00+01:00", "2026-11-08T09:00:00.1234567891Z", 1),
		"timestamp hour shape":       strings.Replace(raw, "2026-11-08T09:00:00+01:00", "2026-11-08T9:00:00Z", 1),
		"timestamp invalid offset":   strings.Replace(raw, "+01:00", "+15:00", 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			store := &captureStore{}
			_, _, err := NewService(store).Submit(context.Background(), Scope{"synthetic-demo", "synthetic-lis"}, []byte(data))
			if !errors.Is(err, internal.ErrInvalid) || store.called {
				t.Fatalf("invalid input reached storage: %v", err)
			}
		})
	}
}
