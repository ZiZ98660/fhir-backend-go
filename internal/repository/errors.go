package repository

import (
	"errors"
	"fmt"
	"go-fhir-phase1/internal"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// BEFORE: return the database error directly.
// NOW: map pgx.ErrNoRows to ErrNotFound and SQLSTATE 23505 (unique violation)
// to ErrConflict. Preserve other errors. The handler need not import pgx to
// choose an HTTP status. errors.Is recognizes wrapped identity; errors.As extracts
// a concrete PgError. OPTIONAL ABSTRACTION: do this mapping inline in each method.
func storageError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return internal.ErrNotFound
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return fmt.Errorf("%w: resource id already exists", internal.ErrConflict)
	}
	return err
}

// BEFORE (old collection loop; inactive):
//
// defer rows.Close()
// var results [][]byte
//
//	for rows.Next() {
//	    var data []byte
//	    if err := rows.Scan(&data); err != nil { continue }
//	    results = append(results, data)
//	}
//
// return results, nil
//
// NOW: pgx.CollectRows closes rows and reports scan/iteration failures.
// RowTo[[]byte] scans one row into a byte slice; CollectRows gathers those slices.
// WHY: continue hid scan failures; the old loop also omitted rows.Err().
// A manual loop that returns scan errors and checks rows.Err() is equally valid.
// Correcting error handling was needed; sharing the helper was optional.
func collectData(rows pgx.Rows) ([][]byte, error) {
	return pgx.CollectRows(rows, pgx.RowTo[[]byte])
}
