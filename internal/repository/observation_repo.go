package repository

import (
	"context"
	"go-fhir-phase1/internal"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ObservationStore interface {
	InitTable(context.Context) error
	SaveObservation(context.Context, internal.Observation, []byte) error
	GetObservationsByPatient(context.Context, string) ([][]byte, error)
	GetObservationByID(context.Context, string) ([]byte, error)
	UpdateObservation(context.Context, internal.Observation, []byte, string) error
	DeleteObservation(context.Context, string, string) error
	GetObservationHistory(context.Context, string) ([]ResourceVersion, error)
	GetObservationVersion(context.Context, string, string) (ResourceVersion, error)
}

type ObservationRepository struct{ pool *pgxpool.Pool }

func NewObservationRepository(pool *pgxpool.Pool) ObservationStore {
	return &ObservationRepository{pool: pool}
}

func (r *ObservationRepository) InitTable(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS observations (
 id TEXT PRIMARY KEY, resource_type TEXT NOT NULL, patient_id TEXT NOT NULL, data JSONB NOT NULL
 )`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE observations ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS observation_history (
 sequence BIGSERIAL PRIMARY KEY,
 resource_id TEXT NOT NULL REFERENCES observations(id),
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
	if _, err := tx.Exec(ctx, `INSERT INTO observation_history (resource_id, version_id, data, method)
 SELECT id, COALESCE(NULLIF(data->'meta'->>'versionId', ''), 'legacy'), data, 'PUT'
 FROM observations WHERE deleted_at IS NULL
 ON CONFLICT (resource_id, version_id) DO NOTHING`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BEFORE: creation inserted only the current resource.
// NOW: current data and its initial snapshot commit together. A missing or deleted
// Patient is rejected, and a shared Patient lock coordinates with cascade deletion.
func (r *ObservationRepository) SaveObservation(ctx context.Context, o internal.Observation, data []byte) error {
	patientID, err := internal.PatientID(o.Subject.Reference)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO observations (id, resource_type, patient_id, data)
 SELECT $1, $2, $3, $4 FROM patients WHERE id = $3 AND deleted_at IS NULL FOR SHARE`, o.ID, o.ResourceType, patientID, data)
	if err != nil {
		return storageError(err)
	}
	if result.RowsAffected() == 0 {
		return internal.Invalid("referenced patient is missing or deleted")
	}
	version := o.Meta.VersionID
	if version == "" {
		version = "legacy"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO observation_history (resource_id, version_id, data, method)
 VALUES ($1, $2, $3, 'POST')`, o.ID, version, data); err != nil {
		return storageError(err)
	}
	return tx.Commit(ctx)
}

func (r *ObservationRepository) GetObservationsByPatient(ctx context.Context, patient string) ([][]byte, error) {
	rows, err := r.pool.Query(ctx, `SELECT data FROM observations WHERE patient_id = $1 AND deleted_at IS NULL`, patient)
	if err != nil {
		return nil, err
	}
	return collectData(rows)
}

func (r *ObservationRepository) GetObservationByID(ctx context.Context, id string) ([]byte, error) {
	var data []byte
	var deleted bool
	err := r.pool.QueryRow(ctx, `SELECT data, deleted_at IS NOT NULL FROM observations WHERE id = $1`, id).Scan(&data, &deleted)
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
func (r *ObservationRepository) UpdateObservation(ctx context.Context, o internal.Observation, data []byte, previousVersion string) error {
	// BEFORE: this method contained the transaction and all three write steps.
	// NOW: named fields describe the update; resource_writes.go performs the steps.
	return updateResource(ctx, r.pool, observationStorage, resourceUpdate{
		ID: o.ID, ResourceType: o.ResourceType, VersionID: o.Meta.VersionID,
		PreviousVersion: previousVersion, Data: data, SubjectReference: o.Subject.Reference,
	})
}

func (r *ObservationRepository) DeleteObservation(ctx context.Context, id, expectedVersion string) error {
	// The shared wrapper locks the Patient first and commits only after its
	// shared soft-delete step has saved both the timestamp and history marker.
	return softDeleteClinicalResource(ctx, r.pool, observationStorage, id, expectedVersion)
}

// This explicit transaction step is shared only by individual deletion and
// Patient cascade deletion; both callers already hold the Patient row lock.
func deleteObservationTx(ctx context.Context, tx pgx.Tx, id, expectedVersion string, at time.Time) error {
	return softDeleteResourceTx(ctx, tx, observationStorage, id, expectedVersion, at)
}

func (r *ObservationRepository) GetObservationHistory(ctx context.Context, id string) ([]ResourceVersion, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM observations WHERE id = $1)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, internal.ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT version_id, data, method, recorded_at FROM observation_history WHERE resource_id = $1 ORDER BY sequence DESC`, id)
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

func (r *ObservationRepository) GetObservationVersion(ctx context.Context, id, version string) (ResourceVersion, error) {
	var entry ResourceVersion
	err := r.pool.QueryRow(ctx, `SELECT version_id, data, method, recorded_at FROM observation_history WHERE resource_id = $1 AND version_id = $2`, id, version).Scan(&entry.VersionID, &entry.Data, &entry.Method, &entry.RecordedAt)
	return entry, storageError(err)
}
