package repository

import (
	"context"
	"errors"
	"go-fhir-phase1/internal"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type brokenRows struct {
	pgx.Rows
	scanError      error
	iterationError error
	next           bool
	closed         bool
}

func (r *brokenRows) Next() bool        { value := r.next; r.next = false; return value }
func (r *brokenRows) Scan(...any) error { return r.scanError }
func (r *brokenRows) Err() error        { return r.iterationError }
func (r *brokenRows) Close()            { r.closed = true }

func TestQueryErrorsAreNotSilentlySkipped(t *testing.T) {
	failure := errors.New("broken rows")
	for _, rows := range []*brokenRows{{next: true, scanError: failure}, {iterationError: failure}} {
		_, err := collectData(rows)
		if !errors.Is(err, failure) || !rows.closed {
			t.Fatalf("err=%v closed=%v", err, rows.closed)
		}
	}
}

func TestStorageErrorClassification(t *testing.T) {
	for _, test := range []struct{ input, want error }{
		{pgx.ErrNoRows, internal.ErrNotFound},
		{&pgconn.PgError{Code: "23505"}, internal.ErrConflict},
		{context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		if err := storageError(test.input); !errors.Is(err, test.want) {
			t.Fatalf("got %v want %v", err, test.want)
		}
	}
}

func TestMalformedReferencesNeverReachDatabase(t *testing.T) {
	ctx := context.Background()
	for _, reference := range []string{"", "P", "Patient/", "Other/p1"} {
		o := internal.Observation{Subject: internal.Reference{Reference: reference}}
		if err := (&ObservationRepository{}).SaveObservation(ctx, o, nil); !errors.Is(err, internal.ErrInvalid) {
			t.Errorf("observation %q: %v", reference, err)
		}
		c := internal.Condition{Subject: internal.Reference{Reference: reference}}
		if err := (&ConditionRepository{}).SaveCondition(ctx, c, nil); !errors.Is(err, internal.ErrInvalid) {
			t.Errorf("condition %q: %v", reference, err)
		}
	}
}
