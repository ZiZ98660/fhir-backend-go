package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
	startupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		log.Fatalf("Failed to apply integration database migrations: %v", err)
	}
	seedIntegrationMappings(startupCtx, pool)
	// One configured synthetic source for this first slice. Scope never comes
	// from incoming JSON; this does not provide multi-tenant authorization.
	integrationService := integration.NewService(integration.NewPostgresStore(pool))
	emrAURL := os.Getenv("EMR_A_URL")
	if emrAURL == "" {
		emrAURL = "http://localhost:8081/fhir"
	}
	integrationService.RegisterConnector(integration.NewHTTPConnector(
		"simulated-emr-a",
		emrAURL,
		os.Getenv("EMR_A_TOKEN"),
		nil,
	))
	integrationHandler := handler.NewIntegrationHandler(
		integrationService,
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

	// Step 7: Background Delivery Worker autonomously polling & delivering jobs
	worker := integration.NewWorker(pool, integrationService)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	worker.Start(workerCtx)

	// Step 8: HL7 v2 MLLP TCP listener on :2575 for lab devices & mobile test client
	mllpAddr := os.Getenv("MLLP_ADDR")
	if mllpAddr == "" {
		mllpAddr = ":2575"
	}
	mllpServer := integration.NewMLLPServer(
		mllpAddr,
		integrationService,
		integration.Scope{TenantID: "synthetic-demo", SourceID: "synthetic-lis"},
	)
	if err := mllpServer.Start(); err != nil {
		log.Printf("Warning: could not bind MLLP listener on %s: %v", mllpAddr, err)
	} else {
		fmt.Printf("HL7 v2 MLLP device listener running on %s\n", mllpAddr)
	}

	serverErr := make(chan error, 1)
	go func() {
		fmt.Println("FHIR R4 learning server running on http://localhost:8080")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Signal trapping for zero-data-loss graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("Server startup failed: %v", err)
	case sig := <-quit:
		fmt.Printf("\nReceived signal %s, initiating graceful shutdown...\n", sig)
	}

	// 1. Drain incoming HTTP connections
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// 2. Stop MLLP TCP listener
	_ = mllpServer.Close()

	// 3. Stop delivery worker and wait for in-flight tasks
	workerCancel()
	worker.Stop()
	log.Println("Graceful shutdown complete.")
}

func seedIntegrationMappings(ctx context.Context, pool *pgxpool.Pool) {
	// Find the target patient ID (preferring patient with identifier 00001)
	var targetPatientID string
	err := pool.QueryRow(ctx, `
		SELECT id FROM patients 
		WHERE deleted_at IS NULL 
		ORDER BY (CASE WHEN data @> '{"identifier":[{"value":"00001"}]}'::jsonb THEN 0 ELSE 1 END), id 
		LIMIT 1
	`).Scan(&targetPatientID)
	if err != nil {
		log.Printf("Notice: no active patient found to seed integration mappings: %v", err)
		return
	}

	// 1. Seed patient mapping
	if _, err := pool.Exec(ctx, `
		INSERT INTO integration_patient_mappings 
			(tenant_id, source_id, identifier_system, identifier_value, patient_id)
		VALUES 
			('synthetic-demo', 'synthetic-lis', 'https://example.org/synthetic-lis/patients', '00001', $1)
		ON CONFLICT (tenant_id, source_id, identifier_system, identifier_value)
		DO UPDATE SET patient_id = EXCLUDED.patient_id
	`, targetPatientID); err != nil {
		log.Printf("Failed to seed integration patient mapping: %v", err)
		return
	}

	// 2. Seed order mapping
	if _, err := pool.Exec(ctx, `
		INSERT INTO integration_order_mappings 
			(tenant_id, source_id, identifier_system, identifier_value, patient_id, order_key)
		VALUES 
			('synthetic-demo', 'synthetic-lis', 'https://example.org/synthetic-lis/orders', 'SYN-ORDER-1', $1, 'known-synthetic-order')
		ON CONFLICT (tenant_id, source_id, identifier_system, identifier_value)
		DO UPDATE SET patient_id = EXCLUDED.patient_id, order_key = EXCLUDED.order_key
	`, targetPatientID); err != nil {
		log.Printf("Failed to seed integration order mapping: %v", err)
		return
	}

	// 3. Reconcile any existing integration messages that were stuck in needs_reconciliation
	tag, err := pool.Exec(ctx, `
		UPDATE integration_messages
		SET state = 'ready_for_transformation',
		    reason = '',
		    patient_id = $1,
		    order_key = 'known-synthetic-order'
		WHERE state = 'needs_reconciliation'
		  AND tenant_id = 'synthetic-demo'
		  AND source_id = 'synthetic-lis'
	`, targetPatientID)
	if err != nil {
		log.Printf("Failed to reconcile existing messages: %v", err)
		return
	}

	log.Printf("Integration mappings initialized for patient %s (reconciled %d pending messages)", targetPatientID, tag.RowsAffected())
}
