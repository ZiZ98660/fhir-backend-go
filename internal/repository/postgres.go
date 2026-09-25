package repository

import (
	"context"

	"go-fhir-phase1/internal"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PatientStore defines the contract for patient data operations.
// Any struct that implements these methods satisfies this interface.
type PatientStore interface {
	InitTable(ctx context.Context) error
	SavePatient(ctx context.Context, p internal.Patient, data []byte) error
	GetPatients(ctx context.Context) ([][]byte, error)
	GetPatientByID(ctx context.Context, id string) ([]byte, error)
}

// Ensure PatientRepository implements PatientStore at compile time.
var _ PatientStore = (*PatientRepository)(nil)

// PatientRepository is the PostgreSQL implementation of the PatientStore.
type PatientRepository struct {
	pool *pgxpool.Pool
}

// NewPatientRepository returns a PatientStore (polymorphism).
func NewPatientRepository(pool *pgxpool.Pool) PatientStore {
	return &PatientRepository{
		pool: pool,
	}
}

func (r *PatientRepository) InitTable(ctx context.Context) error {
	tableQuery := `
	CREATE TABLE IF NOT EXISTS patients (
		id TEXT PRIMARY KEY,
		resource_type TEXT NOT NULL,
		data JSONB NOT NULL
	);`
	_, err := r.pool.Exec(ctx, tableQuery)
	return err
}

func (r *PatientRepository) SavePatient(ctx context.Context, p internal.Patient, data []byte) error {
	query := `INSERT INTO patients (id, resource_type, data) VALUES ($1, $2, $3)`
	_, err := r.pool.Exec(ctx, query, p.ID, p.ResourceType, data)
	return err
}

func (r *PatientRepository) GetPatients(ctx context.Context) ([][]byte, error) {
	rows, err := r.pool.Query(ctx, "SELECT data FROM patients")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results [][]byte
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			continue
		}
		results = append(results, data)
	}
	return results, nil
}

func (r *PatientRepository) GetPatientByID(ctx context.Context, id string) ([]byte, error) {
	var data []byte
	query := "SELECT data FROM patients WHERE id = $1"
	err := r.pool.QueryRow(ctx, query, id).Scan(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}
