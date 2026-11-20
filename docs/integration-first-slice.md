# First integration slice: receive and transform synthetic results

## What runs now

The existing Patient, Observation and Condition routes still run. A separate
integration workflow adds two routes protected by the same bearer token:

| Request | Meaning |
| --- | --- |
| `POST /api/v1/integration/results` | Validate a synthetic result and commit its receipt. |
| `GET /api/v1/integration/results/{receipt-id}` | Read receipt metadata, not clinical payloads. |
| `POST /api/v1/integration/results/{receipt-id}/$transform` | Create and return a persisted FHIR R4 transaction Bundle. |

The intake body is an **application contract**, not FHIR JSON. Send
`Content-Type: application/json`. The checked-in example is
`testdata/integration/synthetic-result.json`.

```sh
curl -i http://localhost:8080/api/v1/integration/results \
  -H "Authorization: Bearer $API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @testdata/integration/synthetic-result.json
```

The shell must already have API_TOKEN exported; the server loading .env does not
export variables into your terminal. Never place an actual token in a committed example.

A new receipt returns 202 with a Location header. This means committed intake,
not successful transformation or delivery. Repeating the same message returns
200, the original receipt ID, and `duplicate: true`. Changed content under an
existing message ID returns 409 and never replaces the original.

## Follow the Go code in this order

1. `internal/handler/integration_handler.go`: HTTP body limit, request and response.
2. `internal/integration/intake.go`: typed decode, validation, UTC normalization,
   decimal preservation and content fingerprint.
3. `internal/integration/postgres.go`: transaction, duplicate detection, explicit
   mappings, receipt and event writes.
4. `internal/migrations/001_integration_intake.sql`: tables and unique constraints.

The service owns the workflow and depends on a two-method Store interface. The
PostgresStore satisfies that interface; unit tests substitute a small fake. This
is ordinary Go dependency injection: pass the dependency into a constructor.

## The deliberately narrow contract

This first scenario accepts only the synthetic `SYN-GLU` code in the example.org
test code system, final status, and explicit UCUM mmol/L units. It rejects other
tests, unit conversions, preliminary results and corrections. That restriction
is application policy, not the complete scope of FHIR laboratory resources.

Patient and order identifiers require both system and value. Values such as
`00001` remain strings; no leading zeros are removed and no identity is inferred
from demographic similarity. The result value is a JSON number, not a quoted
string. A pointer distinguishes missing from zero; json.Number preserves the
decimal spelling. Plain decimals are limited to 18 integer and 12 fractional
digits, without scientific notation. This is a documented demo/persistence limit,
not a physiological reference range. NUL characters, Unicode replacement
characters and invalid UTF-8 are rejected rather than repairing damaged identifiers.
A timestamp must include its timezone and is normalized to UTC. Fractional seconds
are limited to nine digits to prevent Go silently truncating input during hashing.

Unknown fields, duplicate object keys, case-variant duplicate keys, multiple
documents, missing values and unsupported input are rejected before database
access. Numeric spellings such as 5.20 and 5.2 are retained, so they produce
different hashes. This favors safe conflict detection over guessing equivalence.

## Why there are two identifiers for a result message

messageId identifies the transmission. resultId identifies the clinical result.
Both are scoped by institution/source. A different messageId with an already-seen
resultId is held for reconciliation, not treated as a new result or an authorized
correction. An advisory transaction lock makes concurrent receipts for the same
scoped result serialize before checking for prior receipts.

## Identity mapping and states

For this development slice, main.go assigns one fixed synthetic scope:
`synthetic-demo` / `synthetic-lis`. The request cannot select tenant/source. This
is not per-source authentication or a secure multi-tenant deployment.

With no mapping configured, the example returns needs_reconciliation with reason
patient-unmatched. That is the expected safe result, not a storage failure.

Only an explicitly provisioned patient mapping can match a live local Patient.
The order mapping must have the same patient. Each mapping key includes tenant,
source, identifier system and identifier value; its primary key prevents an
ambiguous duplicate mapping.

For a disposable synthetic database, create a synthetic Patient through the
existing API and use its returned ID in both statements below. These are
administrator setup examples, not a new public mapping API:

```sql
INSERT INTO integration_patient_mappings
    (tenant_id, source_id, identifier_system, identifier_value, patient_id)
VALUES ('synthetic-demo', 'synthetic-lis',
        'https://example.org/synthetic-lis/patients', '00001',
        '<returned-synthetic-patient-id>');

INSERT INTO integration_order_mappings
    (tenant_id, source_id, identifier_system, identifier_value, patient_id, order_key)
VALUES ('synthetic-demo', 'synthetic-lis',
        'https://example.org/synthetic-lis/orders', 'SYN-ORDER-1',
        '<returned-synthetic-patient-id>', 'known-synthetic-order');
```

order_key is a configured integration context key, not a newly created FHIR
ServiceRequest and not proof that an external order exists. Only configure it
after establishing the synthetic order context.

Provision mappings before sending the example to exercise ready_for_transformation.
An existing receipt is not silently reprocessed when mappings change. For a new
independent demonstration, change both messageId and resultId. A reconciliation
and replay operation is not implemented yet.

ready_for_transformation means mappings existed when intake committed. It does
not start a worker or promise delivery. The explicit transform endpoint
revalidates the live Patient and both mappings before creating resources.

## Transforming to FHIR

After a receipt reaches `ready_for_transformation`, call:

```sh
curl -i -X POST \
  'http://localhost:8080/api/v1/integration/results/{receipt-id}/$transform' \
  -H "Authorization: Bearer $API_TOKEN"
```

The response is a FHIR R4 `Bundle` of type `transaction`: it contains a final
`Observation` with the measured quantity and a final `DiagnosticReport` whose
`result` points to that Observation. The Bundle uses `PUT` requests and stable
resource IDs, so retrying returns the exact saved Bundle rather than generating
new resource IDs. The server currently stores this Bundle with the intake receipt;
it does not send it to another FHIR server or insert the resources into the
local clinical tables. No `ServiceRequest` or `Specimen` is invented from the
current intake fields.

The Bundle follows the R4 transaction entry and fullUrl reference model, while
the report and observation use their R4 resource fields. This is a focused
synthetic profile, not full FHIR validation or external delivery.

## Transactions, raw data and migrations

Receipt, matching outcome and two audit events commit together. An event failure
rolls back the receipt. Replays create no extra events. The original bytes are
stored in BYTEA, separately from the normalized JSONB payload and SHA-256 hash.
This preserves what arrived without returning it from the status endpoint.
Payloads are sensitive even when the development fixture is synthetic. Use only
synthetic data here; retention, access controls, encrypted deployment/storage,
operator auditing and credential separation remain required before real use.

New numbered migrations are embedded in the binary with go:embed. An advisory
lock prevents two instances racing through setup. A checksum prevents silently
editing a migration already applied. Existing FHIR InitTable methods remain in
place; this slice does not migrate or rewrite existing clinical records. The
runner currently runs at startup, after those methods; a separate migration
deployment command can follow when deployment needs it.

## FHIR R4 models verified against official definitions

The dedicated research agent checked the fixed R4 v4.0.1 pages and examples:

- https://hl7.org/fhir/R4/servicerequest.html
- https://hl7.org/fhir/R4/specimen.html
- https://hl7.org/fhir/R4/diagnosticreport.html
- https://hl7.org/fhir/R4/observation.html
- https://hl7.org/fhir/R4/json.html

Models are in internal/lab_resources.go; selected validation is in
internal/lab_validation.go. ServiceRequest requires status, intent and subject.
Specimen has no resource-specific mandatory base fields. DiagnosticReport requires
status and code; its subject is base optional. Optional supplied subjects are
restricted to local Patients by this application's subset.

DiagnosticReport.basedOn, specimen and result are arrays. Specimen.request is an
array. Observation.specimen is one object. ServiceRequest.specimen is optional;
an order made before collection need not reference a specimen that does not yet
exist. Later Specimen.request can reference that order.

Only selected fields and dateTime choice alternatives are modeled. Invalid
optional values are checked, but validation does not establish target existence,
patient ownership, full datatype constraints, terminology correctness, FHIRPath
invariants, or profile conformance. There are no new standalone CRUD endpoints
for these resources yet. Existing Observation's three new optional linkage fields do not
perform target existence checks either.

testdata/fhir-r4 contains authored synthetic JSON examples, not copies of patient
records. The generated Bundle uses the same selected models. Intake and
Observation quantity use json.Number to preserve decimal spelling. An independent
R4 profile validator has not been run.

Timestamp validation checks lexical shape and calendar validity; the supported
instant subset excludes leap seconds and fractions beyond nine digits even though
base R4 permits them. Optional partial dateTime fields retain their original
precision. These limits are narrower than full FHIR primitive support.

## Tests and next step

Run:

```sh
go test ./...
go test -race ./...
go vet ./...
```

PostgreSQL tests require an explicitly set TEST_DATABASE_URL for a disposable
database with schema-creation permissions.
They test durable retrieval, scope isolation, explicit identity, concurrent
deduplication, result collision handling, migration repeatability/checksums and
rollback when an audit write fails. Without that variable they skip; passing
unit tests is not proof that the SQL was exercised.

Next: add an outbound FHIR client and durable outbox/retry behavior, then send
this transaction Bundle to a configured target FHIR server and record its
transaction response. No delivery queue, retry loop, EMR connector, HL7 parser,
correction workflow, real multi-tenancy, full conformance validator or imaging
support is implemented in this slice.
