package repository

import (
	"context"
	"errors"
	"fmt"
	"go-fhir-phase1/internal"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A transaction double checks the common write steps without requiring PostgreSQL.
// Database rollback and cascade concurrency are covered by the integration tests.
type resourceWriteTx struct {
	pgx.Tx
	data        []byte
	deleted     bool
	readError   error
	queries     []string
	arguments   [][]any
	execErrorAt int
	execError   error
	zeroUpdated bool
}

type resourceWriteRow struct{ tx *resourceWriteTx }

func (r resourceWriteRow) Scan(dest ...any) error {
	if r.tx.readError != nil {
		return r.tx.readError
	}
	*dest[0].(*[]byte) = r.tx.data
	*dest[1].(*bool) = r.tx.deleted
	return nil
}

func (tx *resourceWriteTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	return resourceWriteRow{tx: tx}
}

func (tx *resourceWriteTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.queries = append(tx.queries, query)
	tx.arguments = append(tx.arguments, append([]any(nil), args...))
	if tx.execErrorAt == len(tx.queries) {
		return pgconn.CommandTag{}, tx.execError
	}
	if tx.zeroUpdated && strings.HasPrefix(query, "UPDATE ") {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestSharedUpdateKeepsResourceSpecificHistory(t *testing.T) {
	for _, storage := range []resourceStorage{patientStorage, observationStorage, conditionStorage} {
		t.Run(storage.table, func(t *testing.T) {
			tx := &resourceWriteTx{data: []byte(`{"meta":{"versionId":"2"},"subject":{"reference":"Patient/p1"}}`)}
			change := resourceUpdate{ID: "r1", ResourceType: "test", VersionID: "3", PreviousVersion: "2", SubjectReference: "Patient/p1", Data: []byte(`{"meta":{"versionId":"3"}}`)}
			if err := updateResourceTx(context.Background(), tx, storage, change); err != nil {
				t.Fatal(err)
			}
			if len(tx.queries) != 3 {
				t.Fatalf("expected snapshot, update, snapshot; got %d", len(tx.queries))
			}
			for _, i := range []int{0, 2} {
				if !strings.Contains(tx.queries[i], pgx.Identifier{storage.historyTable}.Sanitize()) || !strings.Contains(tx.queries[i], pgx.Identifier{storage.historyKey}.Sanitize()) {
					t.Fatalf("wrong history mapping: %s", tx.queries[i])
				}
			}
			if !strings.Contains(tx.queries[0], "ON CONFLICT") || !strings.Contains(tx.queries[0], "DO NOTHING") {
				t.Fatal("old snapshots must not be replaced")
			}
			if !strings.HasPrefix(tx.queries[1], "UPDATE "+pgx.Identifier{storage.table}.Sanitize()) || !strings.Contains(tx.queries[1], "deleted_at IS NULL") {
				t.Fatalf("wrong update: %s", tx.queries[1])
			}
			if tx.arguments[1][0] != "r1" || tx.arguments[1][3] != "2" || tx.arguments[2][1] != "3" {
				t.Fatalf("wrong version bindings: %+v", tx.arguments)
			}
		})
	}
}

func TestSharedUpdateRejectsConflictsBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name              string
		storage           resourceStorage
		tx                resourceWriteTx
		previous, subject string
		want              error
	}{
		{"deleted", patientStorage, resourceWriteTx{deleted: true}, "2", "", internal.ErrGone},
		{"missing", patientStorage, resourceWriteTx{readError: pgx.ErrNoRows}, "2", "", internal.ErrNotFound},
		{"stale", patientStorage, resourceWriteTx{data: []byte(`{"meta":{"versionId":"3"}}`)}, "2", "", internal.ErrPrecondition},
		{"observation subject changed", observationStorage, resourceWriteTx{data: []byte(`{"meta":{"versionId":"2"},"subject":{"reference":"Patient/p1"}}`)}, "2", "Patient/p2", internal.ErrInvalid},
		{"condition subject changed", conditionStorage, resourceWriteTx{data: []byte(`{"meta":{"versionId":"2"},"subject":{"reference":"Patient/p1"}}`)}, "2", "Patient/p2", internal.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := updateResourceTx(context.Background(), &test.tx, test.storage, resourceUpdate{ID: "r1", PreviousVersion: test.previous, SubjectReference: test.subject})
			if !errors.Is(err, test.want) || len(test.tx.queries) != 0 {
				t.Fatalf("err=%v writes=%d", err, len(test.tx.queries))
			}
		})
	}
}

func TestSharedSoftDeleteWritesMarkerWithoutRemovingData(t *testing.T) {
	for _, storage := range []resourceStorage{patientStorage, observationStorage, conditionStorage} {
		t.Run(storage.table, func(t *testing.T) {
			tx := &resourceWriteTx{data: []byte(`{"meta":{"versionId":"2"}}`)}
			at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			if err := softDeleteResourceTx(context.Background(), tx, storage, "r1", "2", at); err != nil {
				t.Fatal(err)
			}
			if len(tx.queries) != 3 {
				t.Fatalf("expected three writes, got %d", len(tx.queries))
			}
			if !strings.Contains(tx.queries[1], "SET deleted_at = $2") || tx.arguments[1][1] != at {
				t.Fatal("deletion must set only the deletion timestamp")
			}
			if !strings.Contains(tx.queries[2], "'DELETE'") || tx.arguments[2][1] != "3" || tx.arguments[2][2] != at {
				t.Fatal("wrong deletion marker")
			}
			for _, query := range tx.queries {
				if strings.Contains(query, "DELETE FROM") {
					t.Fatal("soft deletion issued a hard delete")
				}
			}
		})
	}
}

func TestSharedSoftDeleteRejectsStaleAndRepeatsNoOp(t *testing.T) {
	for _, test := range []struct {
		name string
		tx   resourceWriteTx
		want error
	}{
		{"already deleted", resourceWriteTx{deleted: true}, nil},
		{"stale", resourceWriteTx{data: []byte(`{"meta":{"versionId":"3"}}`)}, internal.ErrPrecondition},
		{"missing", resourceWriteTx{readError: pgx.ErrNoRows}, internal.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := softDeleteResourceTx(context.Background(), &test.tx, patientStorage, "r1", "2", time.Now())
			if !errors.Is(err, test.want) || len(test.tx.queries) != 0 {
				t.Fatalf("err=%v writes=%d", err, len(test.tx.queries))
			}
		})
	}
	for _, version := range []string{"bad", "0", "-1", fmt.Sprint(int(^uint(0) >> 1))} {
		tx := &resourceWriteTx{data: []byte(fmt.Sprintf(`{"meta":{"versionId":%q}}`, version))}
		if err := softDeleteResourceTx(context.Background(), tx, patientStorage, "r1", "", time.Now()); err == nil || len(tx.queries) != 0 {
			t.Fatalf("invalid version %s: err=%v writes=%d", version, err, len(tx.queries))
		}
	}
	tx := &resourceWriteTx{data: []byte(`{}`)}
	if err := softDeleteResourceTx(context.Background(), tx, patientStorage, "r1", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if tx.arguments[2][1] != "1" {
		t.Fatal("unversioned legacy resource must receive deletion version 1")
	}
}

func TestSharedWritesPropagateFailuresToTheirTransactionOwner(t *testing.T) {
	failure := errors.New("database write failed")
	for _, operation := range []string{"update", "delete"} {
		for step := 1; step <= 3; step++ {
			t.Run(fmt.Sprintf("%s step %d", operation, step), func(t *testing.T) {
				tx := &resourceWriteTx{data: []byte(`{"meta":{"versionId":"1"}}`), execErrorAt: step, execError: failure}
				var err error
				if operation == "update" {
					err = updateResourceTx(context.Background(), tx, patientStorage, resourceUpdate{ID: "r1", PreviousVersion: "1", VersionID: "2"})
				} else {
					err = softDeleteResourceTx(context.Background(), tx, patientStorage, "r1", "1", time.Now())
				}
				if !errors.Is(err, failure) || len(tx.queries) != step {
					t.Fatalf("err=%v writes=%d", err, len(tx.queries))
				}
			})
		}
	}
	tx := &resourceWriteTx{data: []byte(`{"meta":{"versionId":"1"}}`), zeroUpdated: true}
	if err := updateResourceTx(context.Background(), tx, patientStorage, resourceUpdate{ID: "r1", PreviousVersion: "1"}); !errors.Is(err, internal.ErrPrecondition) || len(tx.queries) != 2 {
		t.Fatalf("zero-row update: err=%v writes=%d", err, len(tx.queries))
	}
}
