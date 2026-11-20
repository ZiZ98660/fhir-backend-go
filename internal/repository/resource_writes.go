package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"go-fhir-phase1/internal"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These private mappings describe existing tables; they never come from a request.
// Table/column names cannot use $1 parameters, so SQL identifiers are quoted below.
// Resource IDs, versions, timestamps, and JSON still use ordinary SQL parameters.
type resourceStorage struct {
	table, historyTable, historyKey string
	patientOwned                    bool
}

var (
	patientStorage     = resourceStorage{table: "patients", historyTable: "patient_history", historyKey: "patient_id"}
	observationStorage = resourceStorage{table: "observations", historyTable: "observation_history", historyKey: "resource_id", patientOwned: true}
	conditionStorage   = resourceStorage{table: "conditions", historyTable: "condition_history", historyKey: "resource_id", patientOwned: true}
)

// Named fields make each typed repository wrapper readable at the call site.
// The service already validated the resource and prepared its new metadata.
type resourceUpdate struct {
	ID, ResourceType, VersionID, PreviousVersion, SubjectReference string
	Data                                                           []byte
}

type storedResource struct {
	deleted bool
	meta    internal.Meta
	subject internal.Reference
}

func lockResource(ctx context.Context, tx pgx.Tx, storage resourceStorage, id string) (storedResource, error) {
	var current storedResource
	var data []byte
	query := fmt.Sprintf(`SELECT data, deleted_at IS NOT NULL FROM %s WHERE id = $1 FOR UPDATE`, pgx.Identifier{storage.table}.Sanitize())
	if err := tx.QueryRow(ctx, query, id).Scan(&data, &current.deleted); err != nil {
		return current, storageError(err)
	}
	if current.deleted {
		return current, nil
	}
	var header struct {
		Meta    internal.Meta
		Subject internal.Reference
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return current, err
	}
	current.meta, current.subject = header.Meta, header.Subject
	return current, nil
}

// BEFORE: three update methods repeated Begin, parent locking, version checks,
// snapshot inserts, UPDATE, and Commit. NOW: typed wrappers pass a resourceUpdate.
// WHY EXTRACT: these database rules must stay consistent across all resource types.
// This is a plain function and a small table mapping, not a generic CRUD framework.
func updateResource(ctx context.Context, pool *pgxpool.Pool, storage resourceStorage, change resourceUpdate) error {
	patientID := ""
	if storage.patientOwned {
		var err error
		patientID, err = internal.PatientID(change.SubjectReference)
		if err != nil {
			return err
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Always lock Patient before its child, matching Patient cascade deletion.
	if storage.patientOwned {
		var deleted bool
		if err := tx.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM patients WHERE id = $1 FOR SHARE`, patientID).Scan(&deleted); err != nil {
			return storageError(err)
		}
		if deleted {
			return internal.ErrGone
		}
	}
	if err := updateResourceTx(ctx, tx, storage, change); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func updateResourceTx(ctx context.Context, tx pgx.Tx, storage resourceStorage, change resourceUpdate) error {
	current, err := lockResource(ctx, tx, storage, change.ID)
	if err != nil {
		return err
	}
	if current.deleted {
		return internal.ErrGone
	}
	if storage.patientOwned && current.subject.Reference != change.SubjectReference {
		return internal.Invalid("subject.reference cannot change on an existing %s", change.ResourceType)
	}
	if current.meta.VersionID != change.PreviousVersion {
		return internal.ErrPrecondition
	}
	if err := keepCurrentSnapshot(ctx, tx, storage, change.ID); err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE %s SET resource_type = $2, data = $3
		WHERE id = $1 AND deleted_at IS NULL AND COALESCE(data->'meta'->>'versionId', '') = $4`, pgx.Identifier{storage.table}.Sanitize())
	result, err := tx.Exec(ctx, query, change.ID, change.ResourceType, change.Data, change.PreviousVersion)
	if err != nil {
		return storageError(err)
	}
	if result.RowsAffected() == 0 {
		return internal.ErrPrecondition
	}
	query = fmt.Sprintf(`INSERT INTO %s (%s, version_id, data, method) VALUES ($1, $2, $3, 'PUT')`, pgx.Identifier{storage.historyTable}.Sanitize(), pgx.Identifier{storage.historyKey}.Sanitize())
	_, err = tx.Exec(ctx, query, change.ID, change.VersionID, change.Data)
	return storageError(err)
}

// Copy the old document in SQL so unknown JSON fields and exact values survive.
// ON CONFLICT keeps an existing snapshot untouched; it does not replace history.
func keepCurrentSnapshot(ctx context.Context, tx pgx.Tx, storage resourceStorage, id string) error {
	query := fmt.Sprintf(`INSERT INTO %s (%s, version_id, data, method)
		SELECT id, COALESCE(NULLIF(data->'meta'->>'versionId', ''), 'legacy'), data, 'PUT'
		FROM %s WHERE id = $1 ON CONFLICT (%s, version_id) DO NOTHING`,
		pgx.Identifier{storage.historyTable}.Sanitize(), pgx.Identifier{storage.historyKey}.Sanitize(), pgx.Identifier{storage.table}.Sanitize(), pgx.Identifier{storage.historyKey}.Sanitize())
	_, err := tx.Exec(ctx, query, id)
	return storageError(err)
}

// Individual clinical deletion locks its Patient first. Patient deletion instead
// owns the whole cascade transaction and calls softDeleteResourceTx directly.
func softDeleteClinicalResource(ctx context.Context, pool *pgxpool.Pool, storage resourceStorage, id, expectedVersion string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var reference string
	query := fmt.Sprintf(`SELECT data->'subject'->>'reference' FROM %s WHERE id = $1`, pgx.Identifier{storage.table}.Sanitize())
	if err := tx.QueryRow(ctx, query, id).Scan(&reference); err != nil {
		return storageError(err)
	}
	patientID, err := internal.PatientID(reference)
	if err != nil {
		return err
	}
	var parentID string
	if err := tx.QueryRow(ctx, `SELECT id FROM patients WHERE id = $1 FOR UPDATE`, patientID).Scan(&parentID); err != nil {
		return storageError(err)
	}
	if err := softDeleteResourceTx(ctx, tx, storage, id, expectedVersion, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BEFORE: Patient and each child repeated snapshot -> deleted_at -> DELETE marker.
// NOW: this common step runs inside the caller's transaction. It never commits,
// so a failed child marker can still roll back an entire Patient cascade.
func softDeleteResourceTx(ctx context.Context, tx pgx.Tx, storage resourceStorage, id, expectedVersion string, at time.Time) error {
	current, err := lockResource(ctx, tx, storage, id)
	if err != nil {
		return err
	}
	if current.deleted {
		return nil
	} // Repeating DELETE must not add another version.
	if expectedVersion != "" && expectedVersion != current.meta.VersionID {
		return internal.ErrPrecondition
	}
	n := 0
	if current.meta.VersionID != "" {
		n, err = strconv.Atoi(current.meta.VersionID)
		if err != nil || n < 1 || n == int(^uint(0)>>1) {
			return fmt.Errorf("invalid stored resource version")
		}
	}
	if err := keepCurrentSnapshot(ctx, tx, storage, id); err != nil {
		return err
	}
	query := fmt.Sprintf(`UPDATE %s SET deleted_at = $2 WHERE id = $1`, pgx.Identifier{storage.table}.Sanitize())
	if _, err := tx.Exec(ctx, query, id, at); err != nil {
		return storageError(err)
	}
	query = fmt.Sprintf(`INSERT INTO %s (%s, version_id, method, recorded_at) VALUES ($1, $2, 'DELETE', $3)`, pgx.Identifier{storage.historyTable}.Sanitize(), pgx.Identifier{storage.historyKey}.Sanitize())
	_, err = tx.Exec(ctx, query, id, strconv.Itoa(n+1), at)
	return storageError(err)
}
