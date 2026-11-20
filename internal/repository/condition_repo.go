package repository

import (
	"context"
	"go-fhir-phase1/internal"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConditionStore interface {
	InitTable(context.Context) error
	SaveCondition(context.Context, internal.Condition, []byte) error
	GetConditionsByPatient(context.Context, string) ([][]byte, error)
	GetConditionByID(context.Context, string) ([]byte, error)
	UpdateCondition(context.Context, internal.Condition, []byte, string) error
	DeleteCondition(context.Context, string, string) error
	GetConditionHistory(context.Context, string) ([]ResourceVersion, error)
	GetConditionVersion(context.Context, string, string) (ResourceVersion, error)
}

type ConditionRepository struct{ pool *pgxpool.Pool }

func NewConditionRepository(pool *pgxpool.Pool) ConditionStore {
	return &ConditionRepository{pool: pool}
}

func (r *ConditionRepository) InitTable(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS conditions (
 id TEXT PRIMARY KEY, resource_type TEXT NOT NULL, data JSONB NOT NULL
 )`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE conditions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS condition_history (
 sequence BIGSERIAL PRIMARY KEY,
 resource_id TEXT NOT NULL REFERENCES conditions(id),
 version_id TEXT NOT NULL,
 data JSONB,
 method TEXT NOT NULL CHECK (method IN ('POST', 'PUT', 'DELETE')),
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE (resource_id, version_id),
 CHECK ((method = 'DELETE' AND data IS NULL) OR (method <> 'DELETE' AND data IS NOT NULL))
 )`); err != nil {
		return err
	}
	// Preserve the surviving legacy snapshot without rewriting its JSON.
	if _, err := tx.Exec(ctx, `INSERT INTO condition_history (resource_id, version_id, data, method)
 SELECT id, COALESCE(NULLIF(data->'meta'->>'versionId', ''), 'legacy'), data, 'PUT'
 FROM conditions WHERE deleted_at IS NULL
 ON CONFLICT (resource_id, version_id) DO NOTHING`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BEFORE: creation inserted only the current resource.
// NOW: current data and its initial snapshot commit together. A missing or deleted
// Patient is rejected, and a shared Patient lock coordinates with cascade deletion.
func (r *ConditionRepository) SaveCondition(ctx context.Context, c internal.Condition, data []byte) error {
	patientID, err := internal.PatientID(c.Subject.Reference)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO conditions (id, resource_type, data)
 SELECT $1, $2, $3 FROM patients WHERE id = $4 AND deleted_at IS NULL FOR SHARE`, c.ID, c.ResourceType, data, patientID)
	if err != nil {
		return storageError(err)
	}
	if result.RowsAffected() == 0 {
		return internal.Invalid("referenced patient is missing or deleted")
	}
	version := c.Meta.VersionID
	if version == "" {
		version = "legacy"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO condition_history (resource_id, version_id, data, method)
 VALUES ($1, $2, $3, 'POST')`, c.ID, version, data); err != nil {
		return storageError(err)
	}
	return tx.Commit(ctx)
}

func (r *ConditionRepository) GetConditionsByPatient(ctx context.Context, patient string) ([][]byte, error) {
	rows, err := r.pool.Query(ctx, `SELECT data FROM conditions WHERE data->'subject'->>'reference' = $1 AND deleted_at IS NULL`, patient)
	if err != nil {
		return nil, err
	}
	return collectData(rows)
}

func (r *ConditionRepository) GetConditionByID(ctx context.Context, id string) ([]byte, error) {
	var data []byte
	var deleted bool
	err := r.pool.QueryRow(ctx, `SELECT data, deleted_at IS NOT NULL FROM conditions WHERE id = $1`, id).Scan(&data, &deleted)
	if err != nil {
		return nil, storageError(err)
	}
	if deleted {
		return nil, internal.ErrGone
	}
	return data, nil
}

// Always lock Patient before the clinical row. Cascade deletion uses the same
// order, preventing an update from racing with a Patient's deletion.
func (r *ConditionRepository) UpdateCondition(ctx context.Context, c internal.Condition, data []byte, previousVersion string) error {
	// Same database algorithm as Patient/Observation, using Condition's table map.
	return updateResource(ctx, r.pool, conditionStorage, resourceUpdate{
		ID: c.ID, ResourceType: c.ResourceType, VersionID: c.Meta.VersionID,
		PreviousVersion: previousVersion, Data: data, SubjectReference: c.Subject.Reference,
	})
}

func (r *ConditionRepository) DeleteCondition(ctx context.Context, id, expectedVersion string) error {
	// The shared wrapper locks the Patient before this Condition, as in a cascade.
	return softDeleteClinicalResource(ctx, r.pool, conditionStorage, id, expectedVersion)
}

// This explicit transaction step is shared only by individual deletion and
// Patient cascade deletion; both callers already hold the Patient row lock.
func deleteConditionTx(ctx context.Context, tx pgx.Tx, id, expectedVersion string, at time.Time) error {
	return softDeleteResourceTx(ctx, tx, conditionStorage, id, expectedVersion, at)
}

func (r *ConditionRepository) GetConditionHistory(ctx context.Context, id string) ([]ResourceVersion, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conditions WHERE id = $1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, internal.ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT version_id, data, method, recorded_at FROM condition_history WHERE resource_id = $1 ORDER BY sequence DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

func (r *ConditionRepository) GetConditionVersion(ctx context.Context, id, version string) (ResourceVersion, error) {
	var entry ResourceVersion
	err := r.pool.QueryRow(ctx, `SELECT version_id, data, method, recorded_at FROM condition_history WHERE resource_id = $1 AND version_id = $2`, id, version).Scan(&entry.VersionID, &entry.Data, &entry.Method, &entry.RecordedAt)
	return entry, storageError(err)
}
