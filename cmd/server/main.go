package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"go-fhir-phase1/internal/handler"
	"go-fhir-phase1/internal/integration"
	"go-fhir-phase1/internal/middleware"
	"go-fhir-phase1/internal/migrations"
	"go-fhir-phase1/internal/repository"
	"go-fhir-phase1/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}
	apiToken := os.Getenv("API_TOKEN")
	if apiToken == "" {
		log.Fatal("API_TOKEN environment variable is not set")
	}

	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()
	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := pool.Ping(startupCtx); err != nil {
		log.Fatalf("Database connectivity check failed: %v", err)
	}

	patientRepo := repository.NewPatientRepository(pool)
	patientService := service.NewPatientService(patientRepo)
	patientHandler := handler.NewPatientHandler(patientService)

	obsRepo := repository.NewObservationRepository(pool)
	obsService := service.NewObservationService(obsRepo)
	obsHandler := handler.NewObservationHandler(obsService)

	condRepo := repository.NewConditionRepository(pool)
	condService := service.NewConditionService(condRepo)
	condHandler := handler.NewConditionHandler(condService)

	if err := patientRepo.InitTable(startupCtx); err != nil {
		log.Fatalf("Failed to initialize patient table: %v", err)
	}
	if err := obsRepo.InitTable(startupCtx); err != nil {
		log.Fatalf("Failed to initialize observation table: %v", err)
	}
	if err := condRepo.InitTable(startupCtx); err != nil {
		log.Fatalf("Failed to initialize condition table: %v", err)
	}
	if err := migrations.Apply(startupCtx, pool); err != nil {
		log.Fatal("Failed to apply integration database migrations")
	}
	// One configured synthetic source for this first slice. Scope never comes
	// from incoming JSON; this does not provide multi-tenant authorization.
	integrationHandler := handler.NewIntegrationHandler(
		integration.NewService(integration.NewPostgresStore(pool)),
		integration.Scope{TenantID: "synthetic-demo", SourceID: "synthetic-lis"},
	)

	mux := http.NewServeMux()
	for path, fn := range map[string]http.HandlerFunc{
		"/api/v1/Patient":             patientHandler.HandlePatient,
		"/api/v1/Observation":         obsHandler.HandleObservation,
		"/api/v1/Condition":           condHandler.HandleCondition,
		"/api/v1/integration/results": integrationHandler.HandleResults,
	} {
		protected := middleware.AuthMiddleware(apiToken, fn)
		mux.HandleFunc(path, protected)
		mux.HandleFunc(path+"/", protected)
	}
	mux.HandleFunc("GET /health", handler.Health(pool))
	server := &http.Server{
		Addr: ":8080", Handler: middleware.RequestTimeout(mux),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	fmt.Println("FHIR R4 learning server running on http://localhost:8080")
	log.Fatal(server.ListenAndServe())
}
