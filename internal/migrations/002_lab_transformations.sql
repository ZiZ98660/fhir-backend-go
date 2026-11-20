-- Persist the FHIR transaction Bundle with the receipt so retries return the
-- same resources and never create a second version of the same synthetic result.
ALTER TABLE integration_messages
    ADD COLUMN transformation_bundle BYTEA,
    ADD COLUMN observation_id TEXT,
    ADD COLUMN diagnostic_report_id TEXT,
    ADD COLUMN transformed_at TIMESTAMPTZ;

ALTER TABLE integration_messages
    DROP CONSTRAINT integration_messages_state_check,
    ADD CONSTRAINT integration_messages_state_check
        CHECK (state IN ('needs_reconciliation', 'ready_for_transformation', 'transformed')),
    ADD CONSTRAINT integration_messages_transformation_check
        CHECK (
            state <> 'transformed'
            OR (
                patient_id IS NOT NULL
                AND order_key IS NOT NULL
                AND transformation_bundle IS NOT NULL
                AND observation_id IS NOT NULL
                AND diagnostic_report_id IS NOT NULL
                AND transformed_at IS NOT NULL
            )
        );

ALTER TABLE integration_events
    DROP CONSTRAINT integration_events_event_type_check,
    ADD CONSTRAINT integration_events_event_type_check
        CHECK (event_type IN ('received', 'needs_reconciliation', 'ready_for_transformation', 'transformed'));
