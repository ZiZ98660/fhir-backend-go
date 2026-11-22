-- Outbox table for reliable at-least-once delivery to external EMR destinations.
CREATE TABLE integration_delivery_jobs (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES integration_messages(id),
    tenant_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    destination TEXT NOT NULL CHECK (length(btrim(destination)) > 0),
    payload BYTEA NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'delivered', 'failed')),
    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 3,
    last_attempt_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    response_status INT,
    response_body TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    delivered_at TIMESTAMPTZ,
    UNIQUE (tenant_id, source_id, message_id, destination)
);

CREATE INDEX integration_delivery_jobs_pending_idx
    ON integration_delivery_jobs (state, next_attempt_at)
    WHERE state = 'pending';

ALTER TABLE integration_events
    DROP CONSTRAINT integration_events_event_type_check,
    ADD CONSTRAINT integration_events_event_type_check
        CHECK (event_type IN (
            'received', 'needs_reconciliation', 'ready_for_transformation',
            'transformed', 'delivery_enqueued', 'delivered', 'delivery_failed'
        ));
