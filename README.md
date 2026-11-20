# Step 1: Minimal Viable FHIR EMR Backend

This branch represents the foundational, minimal implementation of an EMR backend.

## Architectural Concepts
- **Direct Transport & Persistence:** Standard library `net/http` handles HTTP endpoints (`/api/v1/Patient` and `/health`).
- **PostgreSQL JSONB Storage:** Records are stored directly as raw JSONB in a single `patients` table.
- **Minimal FHIR Model:** `internal/patient.go` defines standard FHIR R4 `Patient`, `HumanName`, `Identifier`, and `Address`.
- **Zero Architectural Abstractions:** No repository, service, or middleware layers yet—designed to understand the raw HTTP request/response and database cycle.

## How to Run
```sh
go run ./cmd/server/main.go
```
