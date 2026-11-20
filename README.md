# Step 2: Layered 3-Tier Architecture ("Elite Architecture")

This branch refactors the monolithic Step 1 backend into a clean 3-tier architecture.

## Architectural Concepts
- **Repository Layer (`internal/repository/`):** Manages all database queries, parameterized SQL, and connection pools. Defines the `PatientStore` interface for testability.
- **Service Layer (`internal/service/`):** Owns business rules, input validation, ID assignment, and FHIR `Bundle` search formatting. Completely decoupled from HTTP.
- **Handler Layer (`internal/handler/`):** Maps HTTP requests to service calls, parses query strings, and manages FHIR content types (`application/fhir+json`).
- **Composition Root (`cmd/server/main.go`):** Uses constructor-based dependency injection:
  `Database Pool` $\rightarrow$ `Repository` $\rightarrow$ `Service` $\rightarrow$ `Handler`.

## How to Compare with Step 1
```sh
git diff step-01-minimal-emr
```

## How to Test and Run
```sh
go test ./...
go run ./cmd/server/main.go
```
