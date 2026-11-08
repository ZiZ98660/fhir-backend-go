-- This migration adds the integration inbox; it does not change FHIR resources.
CREATE TABLE integration_patient_mappings (
    tenant_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    identifier_system TEXT NOT NULL,
    identifier_value TEXT NOT NULL,
    patient_id TEXT NOT NULL REFERENCES patients(id),
    PRIMARY KEY (tenant_id, source_id, identifier_system, identifier_value)
);

-- order_key identifies an explicitly configured integration order context.
-- It is NOT a newly created ServiceRequest or proof of a destination order.
CREATE TABLE integration_order_mappings (
    tenant_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    identifier_system TEXT NOT NULL,
    identifier_value TEXT NOT NULL,
    patient_id TEXT NOT NULL REFERENCES patients(id),
    order_key TEXT NOT NULL CHECK (length(btrim(order_key)) > 0),
    PRIMARY KEY (tenant_id, source_id, identifier_system, identifier_value)
);

CREATE TABLE integration_messages (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    result_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    raw_payload BYTEA NOT NULL,
    normalized_payload JSONB NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('needs_reconciliation', 'ready_for_transformation')),
    reason TEXT NOT NULL,
    patient_id TEXT REFERENCES patients(id),
    order_key TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK (state <> 'ready_for_transformation' OR (patient_id IS NOT NULL AND order_key IS NOT NULL)),
    UNIQUE (tenant_id, source_id, message_id)
);

CREATE TABLE integration_events (
    sequence BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES integration_messages(id),
    event_type TEXT NOT NULL CHECK (event_type IN ('received', 'needs_reconciliation', 'ready_for_transformation')),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

-- The result check is separate from message-ID deduplication and needs its own
-- lookup index as the inbox grows.
CREATE INDEX integration_messages_result_lookup
    ON integration_messages (tenant_id, source_id, result_id);
