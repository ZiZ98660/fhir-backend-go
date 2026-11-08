# Go FHIR Learning Server

A deliberately limited FHIR R4 resource API built with Go and PostgreSQL.
The active entry point is `cmd/server`; `cmd/phase4` is an earlier experiment.

## Learning Comparisons

Every current non-test `.go` file has a neighboring comparison file whose name
ends with a literal backtick:

```text
internal/repository/patient_repo.go     runnable shared implementation
internal/repository/patient_repo.go`    explicit comparison implementation
```

The Go tool ignores the backtick filenames during normal builds. The runnable
files keep their implementations. These comparisons are corrected teaching
equivalents of the earlier style, not exact historical snapshots: updates, soft
deletion, version checks, history, safe reference parsing, and Patient cascading
are retained. Unsafe slicing, silent row-scan failures, and fixed IDs are not
reintroduced. Existing matching comparison files were refreshed; no new test
comparison files were created. Older orphan archives without a current `.go`
counterpart are left untouched.

The repository comparisons repeat transactions and literal SQL for each resource,
including snapshots and soft-delete markers. They use explicit row iteration and
database-error checks. Service comparisons repeat concrete decoding, ID/metadata
assignment, Bundle loops, and PUT/PATCH save steps. Handler comparisons repeat
request parsing, body limits, decoding, version-header parsing, and response
writing, with one concrete error writer per resource handler.

Helper-only comparison files (`handler/http.go`, `service/resources.go`,
`repository/resource_writes.go`, `repository/errors.go`, and `outcome.go`, each
ending in a backtick) explain where their logic was expanded instead of simply
duplicating the shared abstraction under another filename. Model definitions,
public interfaces, named error categories, and domain validation methods retain
the same contracts; standard-library and pgx calls are still used.

Start with the two `patient_repo.go` files for the update/soft-delete comparison,
then the two `patient_service.go` files and the two `patient_handler.go` files.
The comparison files form a complete alternative source set and are verified
with a temporary build plus the existing API behavior tests. They are not
automatically selected by the running application.

The active source now includes inactive `BEFORE` excerpts beside the live `NOW`
logic, plus `WHY` explanations. These comments distinguish bug fixes and new
behavior from optional helpers, generics, and file organization. Excerpts may
omit surrounding code or reference removed methods; do not uncomment them as a
replacement implementation. Reconstructed examples are labeled as teaching
equivalents or schematic flow rather than exact historical code.

Start with the POST branch in `internal/handler/patient_handler.go`, follow its
helpers in `internal/handler/http.go`, then follow `CreatePatient` in
`internal/service/patient_service.go` into `internal/repository/patient_repo.go`.
Next compare PUT/PATCH and the version-checked update. Database reference locking
and deletion transactions can be studied after that simpler request flow.
`internal/service/resources.go` explains the generic helpers; `internal/errors.go`,
`internal/outcome.go`, and `internal/repository/errors.go` trace error handling.
The middleware, health handler, clinical handlers/services/repositories, and
`internal/validation.go` contain corresponding comparisons. These annotations
do not change the executable code. `main.go` and the resource structs were left
outside this comparison pass.

## Shared Database Writes

The typed repository methods now delegate their repeated update/soft-delete
steps to `internal/repository/resource_writes.go`:

```text
UpdatePatient / UpdateObservation / UpdateCondition -> updateResource
DeleteObservation / DeleteCondition -> softDeleteClinicalResource
Patient cascade and each resource's deletion -> softDeleteResourceTx
```

The common functions handle transactions, row locks, version checks, preservation
of old snapshots, and writing new snapshots or deletion markers. A small private
table mapping accounts for the different history tables and key columns. SQL
identifiers are quoted, and record values remain parameterized; table mappings
never come from request input. There are no generic type parameters or base classes
in this write abstraction.

Resource validation stays in the existing typed services. Patient deletion still
explicitly walks its Observation and Condition IDs, using the same transaction
for every child and the Patient. The shared transaction steps never commit on
their own, so one failed snapshot can roll back the entire cascade. Read queries,
table initialization, and the API endpoints keep their existing implementations.

## Run

Provide `DATABASE_URL` and `API_TOKEN` through the environment or the existing
local `.env` file. There is no default bearer token. Then run:

```sh
go run ./cmd/server
```

The server listens on port 8080. `/health` is public and pings PostgreSQL with a
two-second deadline. Resource endpoints require `Authorization: Bearer <API_TOKEN>`.
Authentication is still a single shared token; JWT, users, and roles are future work.

## Supported Interactions

| Resource | Collection | Instance |
| --- | --- | --- |
| Patient | POST, GET with `identifier` and/or `search` | GET, PUT, PATCH, DELETE |
| Observation | POST, GET with `patient` | GET, PUT, PATCH, DELETE |
| Condition | POST, GET with `patient` | GET, PUT, PATCH, DELETE |

Collections use `/api/v1/{Resource}`; instances use `/api/v1/{Resource}/{id}`.
The older `?id={id}` form remains supported. Patient filters combine with AND.
`search` is a custom broad JSON text filter, not a standard FHIR search parameter.
Clinical patient searches accept an ID or a local reference such as `Patient/p1`.
Searches return `searchset` Bundles, including `total: 0` for no matches.
Bundle URLs use the request host and TLS state; a proxy must preserve these for
externally correct URLs. Forwarded headers are not trusted automatically.

POST generates a new resource ID and version 1. PUT replaces an existing resource;
its body must include the matching ID and resource type. A missing
target returns 404 rather than creating a new resource. Each successful update
increments `meta.versionId` and updates `meta.lastUpdated`.

PATCH accepts RFC 6902 JSON Patch with `Content-Type: application/json-patch+json`:

```json
[{"op":"add","path":"/active","value":false}]
```

Use the returned ETag in an `If-Match` header, for example `W/"1"`, to reject stale
PUT/PATCH/DELETE requests with 412. Updates also compare versions atomically in SQL.

## Resource History And Soft Deletion

Patient, Observation, and Condition each support soft deletion. DELETE sets
`deleted_at` rather than removing the row. Normal GET returns 410 Gone for a
deleted resource, and ordinary searches exclude it. Unknown IDs return 404.
Repeating DELETE returns 204 without another history entry. PUT/PATCH cannot
restore a deleted resource; a restore operation is not implemented.

Creation, replacement, patching, and deletion retain version history:

```http
GET /api/v1/Patient/{id}/_history
GET /api/v1/Patient/{id}/_history/{version}
GET /api/v1/Observation/{id}/_history
GET /api/v1/Observation/{id}/_history/{version}
GET /api/v1/Condition/{id}/_history
GET /api/v1/Condition/{id}/_history/{version}
```

The first returns a FHIR `history` Bundle, newest first. Each live entry includes
the saved resource, request information, and response version/timestamp. A deletion
entry has no resource body. The second retrieves a particular saved resource with
its original version ETag, even after deletion. Reading the deletion version itself
returns 410. A nonexistent version returns 404. Historical JSON preserves fields
that the current resource struct might not support; it is not decoded into that
struct and silently truncated.

These endpoints use the existing bearer-token protection. They are read-only;
user-specific roles, an audit log of who read history, type/system-wide history,
pagination, and history filters are not implemented. Unsupported history filter
parameters return 400. PATCH produces an update (`PUT`) entry in the timeline.

Snapshots live in `patient_history`, `observation_history`, and `condition_history`.
Current-row writes and their history entries
are saved in the same database transaction, so a history-write failure rolls back
the write too. Old versions cannot be edited through the API.

Deleting a Patient also soft-deletes all their live Observations and Conditions,
identified by `subject.reference`, in the same transaction. Each child gets its
own deletion marker and retains its snapshots. A failed snapshot rolls back the
whole cascade. Other Patients' records are not touched. Deleting a clinical record
individually does not delete its Patient or siblings. New clinical records cannot
reference deleted Patients. Clinical edits cannot change `subject.reference`;
this prevents silently moving an existing result to a different Patient.
Soft deletion is separate from `Patient.active`.

At the next server startup, table initialization adds `deleted_at`, creates
the history tables, and seeds each existing resource's surviving snapshot once.
Imported snapshots are labeled as update entries, since the original operation is
unknown. A snapshot without a version uses the history ID `legacy`, with migration
time as a fallback timestamp if none was stored. Repeated initialization does not
duplicate snapshots. Previously overwritten versions and hard-deleted records
cannot be reconstructed. Current resource JSON is not rewritten by the migration.
MedicationRequest and Encounter have no persisted records/endpoints yet; the
cascade covers all three currently stored resource types, not arbitrary references.

FHIR resource bodies accept `application/fhir+json` or `application/json`, up to
1 MiB. Unsupported fields are rejected instead of silently discarded. Invalid
resources return 400, unsupported content types 415, absent resources 404,
conflicting writes 409, and database failures 500.
Errors use OperationOutcome with severity, code, and diagnostics.

New Observations and Conditions must refer to an existing local Patient. Clinical
writes lock the Patient first, so concurrent Patient deletion cannot leave newly
written clinical records outside the cascade. Existing malformed data is not
rewritten or repaired automatically.

## FHIR Scope

The supported subset uses FHIR R4 JSON names and shapes. Required resource fields,
supported status codes, Patient dates/gender, and modeled choice alternatives
receive focused validation. Patient name is optional under the base specification.
This application's Observation and Condition workflows require a local Patient
subject and a code with text or coding; these are application restrictions.

Quantity/Duration values preserve explicit zero; optional booleans preserve false.
Condition uses R4 recorder/asserter and structured evidence, not R5 participant.
Only string-valued extensions (`url` and `valueString`) are currently supported;
the nonstandard generic `value` key is rejected. Older records using unsupported
fields need an explicit migration before they can safely be edited.
Current reads of malformed or unsupported stored resources fail explicitly with
500, including searches, rather than returning incomplete or silently truncated
data. Historical reads preserve saved JSON instead of applying current field checks.

MedicationRequest has types and validation for core fields and choice elements,
but no handler, service, or persistence yet. This is not a complete FHIR validator:
terminology services, profiles, all datatype constraints, primitive extensions,
and the rest of the full specification remain outside the current subset.

Reference specifications: [FHIR R4 HTTP](https://hl7.org/fhir/R4/http.html),
[JSON](https://hl7.org/fhir/R4/json.html),
[Condition](https://hl7.org/fhir/R4/condition.html), and
[MedicationRequest](https://hl7.org/fhir/R4/medicationrequest.html).

## Verify

```sh
go test ./...
go test -race ./...
go vet ./...
```

Handler tests exercise real services with in-memory stores. PostgreSQL integration
tests run only when `TEST_DATABASE_URL` is set. They create and remove a uniquely
named schema, testing SQL searches, version checks, history migration, transactional
rollback, cascade soft deletion, reference integrity, and a concurrent deletion. Use a
disposable test database; the role needs schema creation
permission. The tests never load the application `.env` automatically.
