// Package migrations applies numbered SQL files. Existing FHIR InitTable methods
// remain in place; only the new integration tables use this runner initially.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serializes migration runners in this database until this transaction ends.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2026110801)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS integration_schema_migrations (
		name TEXT PRIMARY KEY, checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
	)`); err != nil {
		return err
	}
	entries, err := files.ReadDir(".") // embed.ReadDir returns names in sorted order.
	if err != nil {
		return err
	}
	for _, entry := range entries {
		body, err := files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		// Hash the file's bytes so editing an already-applied migration is detected.
		// Schema evolution should add another numbered file, not rewrite history.
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		var saved string
		err = tx.QueryRow(ctx, `SELECT checksum FROM integration_schema_migrations WHERE name = $1`, entry.Name()).Scan(&saved)
		if err == nil {
			if saved != checksum {
				return fmt.Errorf("applied migration %s was changed", entry.Name())
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO integration_schema_migrations (name, checksum) VALUES ($1, $2)`, entry.Name(), checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
