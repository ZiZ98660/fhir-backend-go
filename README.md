# Step 4: Laboratory Integration Engine (Intake & Identity Matching)

This branch introduces an isolated laboratory integration module designed for high-integrity result ingestion, explicit identity matching, and safe retransmission handling without contaminating FHIR CRUD.

## Architectural Concepts
- **Decoupled Module (`internal/integration/`):** Lab exchange logic runs independently from `ObservationService` and FHIR resources.
- **Server-Configured Scoping:** `Scope{TenantID, SourceID}` is pinned by the server environment, preventing tenant-spoofing in request bodies.
- **Data Integrity & Decimal Preservation:** Uses `json.Number` to retain clinical decimals (`5.20`), strict UTF-8 checking, and token-level duplicate key rejection.
- **Durable Idempotency & Concurrency:** PostgreSQL advisory transaction locks serialize submissions of the same result ID. SHA-256 content hashes distinguish genuine retransmissions (`200 OK`) from payload collisions (`409 Conflict`).
- **Explicit Identity Matching:** External identifiers match only via `integration_patient_mappings` and `integration_order_mappings`. Unmatched payloads are safely held in `needs_reconciliation`.
- **Numbered Schema Migrations (`internal/migrations/`):** Migration runner uses PostgreSQL advisory locks and SHA-256 checksums to detect tampered migrations.
- **FHIR R4 Lab Resource Definitions:** Models for `ServiceRequest`, `Specimen`, and `DiagnosticReport` (`internal/lab_resources.go`).

## How to Compare with Step 3
```sh
git diff step-03-clinical-history-auth
```

## How to Test and Run
```sh
go test ./...
go run ./cmd/server/main.go
```
