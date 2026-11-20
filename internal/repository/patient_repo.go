package repository

import (
	"context"
	"go-fhir-phase1/internal"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NOW: UpdatePatient is part of this storage contract, so both PostgreSQL and
// fake stores must offer a conditional update. Go verifies the matching methods;
// there is no inheritance or explicit "implements" declaration.
type PatientStore interface {
	InitTable(ctx context.Context) error
	SavePatient(ctx context.Context, p internal.Patient, data []byte) error
	UpdatePatient(ctx context.Context, p internal.Patient, data []byte, previousVersion string) error
	SearchPatients(ctx context.Context, identifier string, searchTerm string) ([][]byte, error)
	GetPatientByID(ctx context.Context, id string) ([]byte, error)
	DeletePatient(ctx context.Context, id, expectedVersion string) error
	GetPatientHistory(ctx context.Context, id string) ([]ResourceVersion, error)
	GetPatientVersion(ctx context.Context, id, version string) (ResourceVersion, error)
}

// A history row is a saved snapshot, or a DELETE marker with no resource body.
type ResourceVersion struct {
	VersionID  string
	Data       []byte
	Method     string
	RecordedAt time.Time
}

type PatientRepository struct {
	pool *pgxpool.Pool
}

func NewPatientRepository(pool *pgxpool.Pool) PatientStore {
	return &PatientRepository{pool: pool}
}

func (r *PatientRepository) InitTable(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tableQuery := `
	CREATE TABLE IF NOT EXISTS patients (
		id TEXT PRIMARY KEY,
		resource_type TEXT NOT NULL,
		data JSONB NOT NULL
	);`
	if _, err := tx.Exec(ctx, tableQuery); err != nil {
		return err
	}
	// BEFORE: the Patient table held only the current document.
	// NOW: retain that row on deletion and keep immutable snapshots separately.
	if _, err := tx.Exec(ctx, `ALTER TABLE patients ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS patient_history (
		sequence BIGSERIAL PRIMARY KEY,
		patient_id TEXT NOT NULL REFERENCES patients(id),
		version_id TEXT NOT NULL,
		data JSONB,
		method TEXT NOT NULL CHECK (method IN ('POST', 'PUT', 'DELETE')),
		recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
		UNIQUE (patient_id, version_id),
		CHECK ((method = 'DELETE' AND data IS NULL) OR (method <> 'DELETE' AND data IS NOT NULL))
	)`); err != nil {
		return err
	}
	// Seed the surviving version on upgrade. Previously overwritten/deleted data
	// cannot be recovered. 'legacy' identifies a snapshot with no existing version.
	if _, err := tx.Exec(ctx, `INSERT INTO patient_history (patient_id, version_id, data, method)
		SELECT id, COALESCE(NULLIF(data->'meta'->>'versionId', ''), 'legacy'), data, 'PUT'
		FROM patients WHERE deleted_at IS NULL
		ON CONFLICT (patient_id, version_id) DO NOTHING`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BEFORE (SQL excerpt; inactive):
// INSERT INTO patients (id, resource_type, data) VALUES ($1, $2, $3)
// ON CONFLICT (id) DO UPDATE SET data = $3
// NOW: an INSERT only creates; a duplicate key becomes a conflict.
// WHY: an accidental duplicate ID must not silently turn POST into replacement.
// This is behavior, not abstraction. Parameters still separate values from SQL.
func (r *PatientRepository) SavePatient(ctx context.Context, p internal.Patient, data []byte) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	query := `INSERT INTO patients (id, resource_type, data) VALUES ($1, $2, $3)`
	if _, err := tx.Exec(ctx, query, p.ID, p.ResourceType, data); err != nil {
		return storageError(err)
	}
	version := p.Meta.VersionID
	if version == "" {
		version = "legacy"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO patient_history (patient_id, version_id, data, method)
		VALUES ($1, $2, $3, 'POST')`, p.ID, version, data); err != nil {
		return storageError(err)
	}
	return tx.Commit(ctx)
}

// BEFORE: no separate conditional update; saves used upsert.
// NOW: update only when ID and the OLD stored version both match.
// WHY SQL TOO: a Go version check followed by unconditional UPDATE leaves a gap
// for another request to write. Here A and B can read version 1, but after A writes
// version 2, B's WHERE version = '1' matches nothing. RowsAffected() detects that.
// COALESCE handles an absent old version as "" for legacy rows.
// Without client If-Match this catches changes since the service's read, not an
// older copy the client edited before sending its request. This is added behavior.
// History adds a transaction around the write: keep the old snapshot, update,
// save the new snapshot, then commit. If any step fails, none of it is committed.
func (r *PatientRepository) UpdatePatient(ctx context.Context, p internal.Patient, data []byte, previousVersion string) error {
	// NOW: the transaction described above lives in resource_writes.go and is
	// shared with clinical updates. This wrapper identifies the Patient's tables.
	return updateResource(ctx, r.pool, patientStorage, resourceUpdate{
		ID: p.ID, ResourceType: p.ResourceType, VersionID: p.Meta.VersionID,
		PreviousVersion: previousVersion, Data: data,
	})
}

func (r *PatientRepository) SearchPatients(ctx context.Context, identifier string, searchTerm string) ([][]byte, error) {
	var rows pgx.Rows
	var err error

	// BEFORE (branch structure):
	// if identifier != "" { ... } else if searchTerm != "" { ... } else { ... }
	// Both supplied meant the identifier branch ignored searchTerm.
	// NOW: the first branch combines predicates with AND. The broad text filter
	// remains custom search over JSON. This is behavior, not an abstraction.
	if identifier != "" && searchTerm != "" {
		query := `SELECT data FROM patients WHERE deleted_at IS NULL AND data @> jsonb_build_object('identifier', jsonb_build_array(jsonb_build_object('value', $1::text))) AND data::text ILIKE $2 ORDER BY id`
		rows, err = r.pool.Query(ctx, query, identifier, "%"+searchTerm+"%")
	} else if identifier != "" {
		// EXACT SEARCH: Using JSONB containment for identifiers
		query := `SELECT data FROM patients WHERE deleted_at IS NULL AND data @> jsonb_build_object('identifier', jsonb_build_array(jsonb_build_object('value', $1::text)))`
		rows, err = r.pool.Query(ctx, query, identifier)
	} else if searchTerm != "" {
		// FUZZY SEARCH: Cast JSONB to TEXT and use ILIKE for partial, case-insensitive matches
		// This searches across the entire JSON blob for the search term.
		query := `SELECT data FROM patients WHERE deleted_at IS NULL AND data::text ILIKE $1`
		rows, err = r.pool.Query(ctx, query, "%"+searchTerm+"%")
	} else {
		// RETURN ALL
		rows, err = r.pool.Query(ctx, "SELECT data FROM patients WHERE deleted_at IS NULL")
	}

	if err != nil {
		return nil, err
	}
	// BEFORE: rows.Next(), Scan(&data), and continue on scan errors.
	// NOW: collection reports failures and closes rows. repository/errors.go
	// contains the fuller commented-out old loop and the manual alternative.
	return collectData(rows)
}

func (r *PatientRepository) GetPatientByID(ctx context.Context, id string) ([]byte, error) {
	var data []byte
	var deleted bool
	query := "SELECT data, deleted_at IS NOT NULL FROM patients WHERE id = $1"
	err := r.pool.QueryRow(ctx, query, id).Scan(&data, &deleted)
	if err != nil {
		return nil, storageError(err)
	}
	if deleted {
		return nil, internal.ErrGone
	}
	return data, nil
}

// BEFORE (simple deletion; inactive):
// query := "DELETE FROM patients WHERE id = $1"
// _, err := r.pool.Exec(ctx, query, id)
// return err
//
// NOW: lock Patient -> soft-delete linked clinical records -> retain the Patient
// snapshot -> mark Patient deleted -> commit. All history is preserved together.
// Clinical writes lock Patient first too, so a new result cannot escape the cascade.
// A transaction groups the steps; it is not another application layer. These
// clinical-reference checks cover our repository paths, not arbitrary SQL. History
// snapshots also have a foreign key preventing accidental removal of their parent.
func (r *PatientRepository) DeletePatient(ctx context.Context, id, expectedVersion string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	// Any early return abandons uncommitted work. After successful Commit, this
	// deferred Rollback cannot undo the committed deletion; its error is ignored.
	defer tx.Rollback(ctx)

	current, err := lockResource(ctx, tx, patientStorage, id)
	if err != nil {
		return err
	}
	if current.deleted {
		return nil
	}
	if expectedVersion != "" && expectedVersion != current.meta.VersionID {
		return internal.ErrPrecondition
	}
	deletedAt := time.Now().UTC()
	// Read IDs explicitly, close the cursor, then delete each child in this same
	// transaction. Already-deleted children are excluded, so no extra markers appear.
	rows, err := tx.Query(ctx, `SELECT id FROM observations WHERE patient_id = $1 AND deleted_at IS NULL ORDER BY id`, id)
	if err != nil {
		return err
	}
	var observationIDs []string
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			rows.Close()
			return err
		}
		observationIDs = append(observationIDs, childID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, childID := range observationIDs {
		if err := deleteObservationTx(ctx, tx, childID, "", deletedAt); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `SELECT id FROM conditions WHERE data->'subject'->>'reference' = $1 AND deleted_at IS NULL ORDER BY id`, "Patient/"+id)
	if err != nil {
		return err
	}
	var conditionIDs []string
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			rows.Close()
			return err
		}
		conditionIDs = append(conditionIDs, childID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, childID := range conditionIDs {
		if err := deleteConditionTx(ctx, tx, childID, "", deletedAt); err != nil {
			return err
		}
	}
	// The same soft-delete step runs for the Patient after its children.
	if err := softDeleteResourceTx(ctx, tx, patientStorage, id, expectedVersion, deletedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// History is readable even when the current Patient is soft-deleted.
func (r *PatientRepository) GetPatientHistory(ctx context.Context, id string) ([]ResourceVersion, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patients WHERE id = $1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, internal.ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT version_id, data, method, recorded_at FROM patient_history WHERE patient_id = $1 ORDER BY sequence DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Explicit iteration keeps the row/scan/error handling visible for learning.
	var versions []ResourceVersion
	for rows.Next() {
		var version ResourceVersion
		if err := rows.Scan(&version.VersionID, &version.Data, &version.Method, &version.RecordedAt); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

func (r *PatientRepository) GetPatientVersion(ctx context.Context, id, version string) (ResourceVersion, error) {
	var entry ResourceVersion
	err := r.pool.QueryRow(ctx, `SELECT version_id, data, method, recorded_at FROM patient_history WHERE patient_id = $1 AND version_id = $2`, id, version).Scan(&entry.VersionID, &entry.Data, &entry.Method, &entry.RecordedAt)
	return entry, storageError(err)
}
