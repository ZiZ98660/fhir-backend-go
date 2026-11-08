package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go-fhir-phase1/internal"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

/*
Create a file at cmd/phase2/main.go. Your goal is to implement these three things using only the standard net/http package:

1. The Listener: Make the server run on port :8080.
2. The POST Endpoint (/api/v1/Patient):
   - Read the JSON body.
   - Unmarshal it into the internal.Patient struct (import it from go-fhir-phase1/internal).
   - Print the patient's name to the terminal.
   - Return a 201 Created status.
3. The GET Endpoint (/api/v1/Patient/123):
   - Return a hardcoded internal.Patient object as JSON.
   - Return a 200 OK status.

Pro-Tip for your implementation:
Since you are using the standard library, remember that http.HandleFunc is your best friend for routing.
*/

func handlePatient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-type", "application/json")

	switch r.Method {
	case http.MethodGet:
		genderMale := "male"
		isActive := true
		mockPatient := internal.Patient{
			ResourceType: "Patient",
			ID:           "123",
			Active:       &isActive,
			Gender:       &genderMale,
			GivenNames:   []string{"Alex", "Hunter"},
			FamilyName:   "Jones",
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockPatient)

	case http.MethodPost:
		var incomingPatient internal.Patient

		err := json.NewDecoder(r.Body).Decode(&incomingPatient)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "Invalid JSON payload"}`))
			return
		}
		fmt.Printf("[Server Log] successfully Received patient: %s\n", incomingPatient.FamilyName)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(incomingPatient)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)

		w.Write([]byte(`{"error": "status not allowed"}`))
	}

}

// func main() {
// 	http.HandleFunc("/api/v1/Patient", handlePatient)

// 	err := http.ListenAndServe(":8080", nil)

// 	if err != nil {
// 		fmt.Printf("something happened: %v\n", err)
// 	}

// }

var dBasePool *pgxpool.Pool

func initDb() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("No .env file found")
	}

	connStr := os.Getenv("DATABASE_URL")

	if connStr == "" {
		log.Fatal("env variable not set")
	}

	// connect w dB
	var dbErr error
	dBasePool, dbErr = pgxpool.New(context.Background(), connStr)
	if dbErr != nil {
		log.Fatalf("Unable to connect to db: %v\n", dbErr)
	}

	// exec init db query
	tabQuery := `
		CREATE TABLE IF NOT EXISTS patients(
		id TEXT PRIMARY KEY,
		resource_type TEXT NOT NULL,
		data JSONB NOT NULL
)
	`
	var queryErr error

	_, queryErr = dBasePool.Exec(context.Background(), tabQuery)

	if queryErr != nil {
		log.Fatalf("Unable to create table:%v\n", queryErr)
	}

	fmt.Println("db connected successfully and table initialized")
}

func handlePat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		patientID := r.URL.Query().Get("id")
		var getDataQuery string
		var args []interface{}
		// get data from db query
		if patientID == "" {
			getDataQuery = "SELECT data FROM patients"
			args = []interface{}{}
		} else {
			getDataQuery = "SELECT data FROM patients WHERE id = $1"
			args = append(args, patientID)
		}

		rows, err := dBasePool.Query(context.Background(), getDataQuery, args...)

		if err != nil {
			http.Error(w, `{"error": "failed to query db"}`, http.StatusInternalServerError)
			return
		}

		defer rows.Close()

		ptList := []internal.Patient{}
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
			json.NewEncoder(w).Encode(map[string]string{"error": "patient not found"})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ptList)

	case http.MethodPost:
		var newPt internal.Patient
		if err := json.NewDecoder(r.Body).Decode(&newPt); err != nil {
			http.Error(w, `{"error" : "Invalid JSON payload"}`, http.StatusBadRequest)
			return
		}

		newPt.ID = fmt.Sprintf("pt-%d", 3451)

		if newPt.FamilyName == "" {
			http.Error(w, `{"error" : "Family name required"}`, http.StatusBadRequest)
			return
		}

		jsonBytes, err := json.Marshal(&newPt)
		if err != nil {
			http.Error(w, `{"error" : "Internal Server Error"}`, http.StatusInternalServerError)
			return
		}

		insertQuery := `INSERT INTO patients (id, resource_type, data) VALUES ($1, $2, $3)`

		_, err = dBasePool.Exec(context.Background(), insertQuery, newPt.ID, newPt.ResourceType, jsonBytes)

		if err != nil {
			http.Error(w, `{"error" : "Unable to create new pt"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(newPt)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}

}
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if dBasePool == nil {
		http.Error(w, `{
			"status":   "down",
			"database": "disconnected"
			}`, http.StatusInternalServerError)
		return

	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "up",
		"database": "connected",
	})
}

func main() {

	initDb()
	defer dBasePool.Close()

	http.HandleFunc("/api/v1/Patient", handlePat)

	http.HandleFunc("/health", handleHealth)

	fmt.Println("EMR Backend Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))

}
