# Step 6: Outbox Pattern & EMR Connectors

This branch introduces reliable outbound delivery to an external EMR using the Transactional Outbox pattern, Go interfaces for connectors, and simulated EMR testing with `net/http/httptest`.

## Architectural Concepts
- **Transactional Outbox (`internal/migrations/003_integration_outbox.sql`):** When a receipt is transformed, an outbound delivery job is atomically enqueued into `integration_delivery_jobs` in the *same* database transaction as the transformation. Delivery is guaranteed to be retried if the process terminates before or during outbound delivery.
- **Connector Interface (`internal/integration/connector.go`):** `Connector` defines `Deliver(ctx, bundle) (DeliveryResult, error)`. This decouples the core engine from external EMR quirks, authentication headers, and network protocols.
- **HTTP Connector (`HTTPConnector`):** Implements `Connector` over HTTP, adding `Content-Type: application/fhir+json`, Bearer authentication, and bounded response reading.
- **Simulated EMR with `httptest` (`internal/integration/connector_test.go`):** Uses standard library `httptest.Server` to mock an external hospital EMR, asserting that the emitted FHIR transaction bundle is received correctly.
- **Delivery Endpoint:** `POST /api/v1/integration/results/{id}/$deliver?destination=simulated-emr-a`.

## How to Compare with Step 5
```sh
git diff step-05-fhir-transformation
```

## How to Test and Run
```sh
go test ./...
go run ./cmd/server/main.go
```
