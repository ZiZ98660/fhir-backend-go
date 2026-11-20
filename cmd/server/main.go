package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"go-fhir-phase1/internal/handler"
	"go-fhir-phase1/internal/repository"
	"go-fhir-phase1/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	// 1. Setup Environment
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	// 2. Create the Connection Pool
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()

	// 3. Initialize the layers (The "Elite" Wiring)
	// Pool -> Repository -> Service -> Handler
	patientRepo := repository.NewPatientRepository(pool)
	patientService := service.NewPatientService(patientRepo)
	patientHandler := handler.NewPatientHandler(patientService)

	// 4. Ensure the Database Table exists
	if err := patientRepo.InitTable(context.Background()); err != nil {
		log.Fatalf("Failed to initialize database table: %v", err)
	}

	fmt.Println("🚀 Elite Architecture Initialized!")

	// 5. Setup Routing
	http.HandleFunc("/api/v1/Patient", patientHandler.HandlePatient)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if pool == nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"status": "down", "database": "disconnected"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "up", "database": "connected"}`)
	})

	fmt.Println("EMR Backend Server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
