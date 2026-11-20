package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"go-fhir-phase1/internal"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

var dBasePool *pgxpool.Pool

func initDb() {
	_ = godotenv.Load()

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	var err error
	dBasePool, err = pgxpool.New(context.Background(), connStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}

	tabQuery := `
	CREATE TABLE IF NOT EXISTS patients (
		id TEXT PRIMARY KEY,
		resource_type TEXT NOT NULL,
		data JSONB NOT NULL
	);`

	_, err = dBasePool.Exec(context.Background(), tabQuery)
	if err != nil {
		log.Fatalf("Unable to create table: %v", err)
	}

	fmt.Println("Database connected successfully and table initialized")
}

func handlePatient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		patientID := r.URL.Query().Get("id")
		var getDataQuery string
		var args []any

		if patientID == "" {
			getDataQuery = "SELECT data FROM patients"
			args = []any{}
		} else {
			getDataQuery = "SELECT data FROM patients WHERE id = $1"
			args = append(args, patientID)
		}

		rows, err := dBasePool.Query(r.Context(), getDataQuery, args...)
		if err != nil {
			http.Error(w, `{"error": "failed to query database"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var ptList []internal.Patient
		for rows.Next() {
			var dtBytes []byte
			if err := rows.Scan(&dtBytes); err != nil {
				continue
			}
			var p internal.Patient
			if err := json.Unmarshal(dtBytes, &p); err != nil {
				continue
			}
			ptList = append(ptList, p)
		}

		if len(ptList) == 0 && patientID != "" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "patient not found"})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ptList)

	case http.MethodPost:
		var newPt internal.Patient
		if err := json.NewDecoder(r.Body).Decode(&newPt); err != nil {
			http.Error(w, `{"error": "Invalid JSON payload"}`, http.StatusBadRequest)
			return
		}

		if newPt.ID == "" {
			newPt.ID = "pt-3451"
		}
		if newPt.ResourceType == "" {
			newPt.ResourceType = "Patient"
		}

		if len(newPt.Name) == 0 || newPt.Name[0].Family == "" {
			http.Error(w, `{"error": "Family name required"}`, http.StatusBadRequest)
			return
		}

		jsonBytes, err := json.Marshal(&newPt)
		if err != nil {
			http.Error(w, `{"error": "Internal Server Error"}`, http.StatusInternalServerError)
			return
		}

		insertQuery := `INSERT INTO patients (id, resource_type, data) VALUES ($1, $2, $3)`
		_, err = dBasePool.Exec(r.Context(), insertQuery, newPt.ID, newPt.ResourceType, jsonBytes)
		if err != nil {
			http.Error(w, `{"error": "Unable to create new patient"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(newPt)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if dBasePool == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "down",
			"database": "disconnected",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "up",
		"database": "connected",
	})
}

func main() {
	initDb()
	defer dBasePool.Close()

	http.HandleFunc("/api/v1/Patient", handlePatient)
	http.HandleFunc("/health", handleHealth)

	fmt.Println("EMR Backend Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
