# Step 5: FHIR Laboratory Transformation Bundle

This branch adds the transformation engine that converts accepted synthetic lab receipts (`ready_for_transformation`) into deterministic, standard FHIR R4 Transaction Bundles.

## Architectural Concepts
- **Deterministic Resource IDs & UUIDs (`internal/integration/transform.go`):** Computes reproducible IDs and RFC 4122 version-5 UUIDs (`urn:uuid:...`) so identical submissions produce identical bundles without creating duplicate clinical entities.
- **Atomic FHIR Transaction Bundle:** Packages a validated `Observation` and `DiagnosticReport` using `PUT` entries, linking the report's `result` reference directly to the observation.
- **Safe Live Verification:** Rechecks that the matched Patient exists and is not soft-deleted (`p.deleted_at IS NULL`) before generating FHIR resources.
- **Schema Migration `002_lab_transformations.sql`:** Stores the generated bundle (`transformation_bundle BYTEA`), updates message state constraints (`transformed`), and enforces non-null references.
- **Transformation Endpoint:** `POST /api/v1/integration/results/{id}/$transform` in `internal/handler/integration_handler.go`.

## How to Compare with Step 4
```sh
git diff step-04-integration-intake
```

## How to Test and Run
```sh
go test ./...
go run ./cmd/server/main.go
```
