package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// =============================================================================
// MODELS (Equivalent to internal/patient.go)
// =============================================================================

type Patient struct {
	ResourceType string       `json:"resourceType"`
	ID           string       `json:"id,omitempty"`
	Identifier   []Identifier `json:"identifier,omitempty"`
	Active       bool         `json:"active"`
	Name         []HumanName  `json:"name,omitempty"`
	Gender       string       `json:"gender,omitempty"`
	BirthDate    string       `json:"birthDate,omitempty"`
	Address      []Address    `json:"address,omitempty"`
}

type Identifier struct {
	System string `json:"system,omitempty"`
	Value  string `json:"value,omitempty"`
}

type HumanName struct {
	Family string   `json:"family,omitempty"`
	Given  []string `json:"given,omitempty"`
	Use    string   `json:"use,omitempty"`
}

type Address struct {
	Line       []string `json:"line,omitempty"`
	City       string   `json:"city,omitempty"`
	District   string   `json:"district,omitempty"`
	State      string   `json:"state,omitempty"`
	PostalCode string   `json:"postalCode,omitempty"`
	Country    string   `json:"country,omitempty"`
}

type Bundle struct {
	ResourceType string        `json:"resourceType"`
	Type         string        `json:"type"`
	Total        int           `json:"total,omitempty"`
	Entry        []BundleEntry `json:"entry,omitempty"`
}

type BundleEntry struct {
	FullUrl  string      `json:"fullUrl,omitempty"`
	Resource interface{} `json:"resource,omitempty"`
}

// =============================================================================
// REPOSITORY LAYER (Equivalent to internal/repository/postgres.go)
// =============================================================================

type PatientRepository struct {
	pool *pgxpool.Pool
}

func (r *PatientRepository) InitTable() error {
	query := `CREATE TABLE IF NOT EXISTS patients (id TEXT PRIMARY KEY, resource_type TEXT NOT NULL, data JSONB NOT NULL);`
	_, err := r.pool.Exec(context.Background(), query)
	return err
}

func (r *PatientRepository) SavePatient(p Patient, data []byte) error {
	query := `INSERT INTO patients (id, resource_type, data) VALUES ($1, $2, $3)`
	_, err := r.pool.Exec(context.Background(), query, p.ID, p.ResourceType, data)
	return err
}

func (r *PatientRepository) GetPatients() ([][]byte, error) {
	rows, err := r.pool.Query(context.Background(), "SELECT data FROM patients")
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

func (r *PatientRepository) GetPatientByID(id string) ([]byte, error) {
	var data []byte
	query := "SELECT data FROM patients WHERE id = $1"
	err := r.pool.QueryRow(context.Background(), query, id).Scan(&data)
	return data, err
}

// =============================================================================
// SERVICE LAYER (Equivalent to internal/service/patient_service.go)
// =============================================================================

type PatientService struct {
	repo *PatientRepository
}

func (s *PatientService) CreatePatient(p Patient) (Patient, error) {
	if len(p.Name) == 0 || p.Name[0].Family == "" {
		return Patient{}, errors.New("at least one name with a family name is required")
	}
	p.ID = "pt-fhir-123"
	data, _ := json.Marshal(p)
	err := s.repo.SavePatient(p, data)
	return p, err
}

func (s *PatientService) SearchPatients() (Bundle, error) {
	dataList, err := s.repo.GetPatients()
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{
		ResourceType: "Bundle",
		Type:         "search",
		Total:        len(dataList),
	}
	for _, data := range dataList {
		var p Patient
		json.Unmarshal(data, &p)
		bundle.Entry = append(bundle.Entry, BundleEntry{
			FullUrl:  "http://localhost:8080/api/v1/Patient/" + p.ID,
			Resource: p,
		})
	}
	return bundle, nil
}

func (s *PatientService) GetPatientByID(id string) (Patient, error) {
	data, err := s.repo.GetPatientByID(id)
	if err != nil {
		return Patient{}, err
	}
	var p Patient
	json.Unmarshal(data, &p)
	return p, nil
}

// =============================================================================
// HANDLER LAYER (Equivalent to internal/handler/patient_handler.go)
// =============================================================================

type PatientHandler struct {
	service *PatientService
}

func (h *PatientHandler) HandlePatient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/fhir+json")
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		if id == "" {
			bundle, _ := h.service.SearchPatients()
			json.NewEncoder(w).Encode(bundle)
		} else {
			p, err := h.service.GetPatientByID(id)
			if err != nil {
				http.Error(w, "Not Found", 404)
				return
			}
			json.NewEncoder(w).Encode(p)
		}
	} else if r.Method == http.MethodPost {
		var p Patient
		json.NewDecoder(r.Body).Decode(&p)
		created, err := h.service.CreatePatient(p)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(created)
	}
}

// =============================================================================
// MAIN (The Assembler)
// =============================================================================

func main() {
	godotenv.Load()
	pool, _ := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))

	repo := &PatientRepository{pool: pool}
	repo.InitTable()

	svc := &PatientService{repo: repo}
	hdl := &PatientHandler{service: svc}

	http.HandleFunc("/api/v1/Patient", hdl.HandlePatient)
	fmt.Println("Linear Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
