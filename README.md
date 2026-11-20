# Step 3: FHIR Production Hardening, Auth, & Multi-Resource History

This branch introduces production healthcare standards: security, standardized error handling, optimistic concurrency, immutable audit history, and clinical resource expansions.

## Architectural Concepts
- **Authentication & Middleware (`internal/middleware/`):** Bearer token validation and request context timeouts.
- **Standardized FHIR Error Outcomes (`internal/outcome.go` & `internal/errors.go`):** Translates Go sentinel errors (`ErrNotFound`, `ErrConflict`, etc.) into FHIR `OperationOutcome` responses.
- **HTTP Transport Hardening (`internal/handler/http.go`):** 1 MiB body protection (`MaxBytesReader`), weak ETag parsing (`If-Match: W/"..."`), and generic resource decoder `decodeResource[T]`.
- **Audit History & Soft Deletion:** `patient_history`, `observation_history`, and `condition_history` tables tracking every version (`versionId`, `lastUpdated`). Records are soft-deleted via `deleted_at`.
- **Multi-Resource Clinical Expansion:** Introduces `Observation` and `Condition` using shared repository writes (`internal/repository/resource_writes.go`).
- **Comprehensive Testing:** End-to-end HTTP tests, concurrency tests, and database lifecycle verification.

## How to Compare with Step 2
```sh
git diff step-02-layered-architecture
```

## How to Test and Run
```sh
go test ./...
go run ./cmd/server/main.go
```
