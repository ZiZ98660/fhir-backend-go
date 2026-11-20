package integration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go-fhir-phase1/internal"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Receive(ctx context.Context, scope Scope, message Submission) (Receipt, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Receipt{}, false, err
	}
	defer tx.Rollback(ctx)
	// INSERT/ON CONFLICT serializes concurrent submissions of the same message.
	// No receipt is returned until its transaction and audit event are committed.
	id := "msg-" + rand.Text()
	var receipt Receipt
	err = tx.QueryRow(ctx, `INSERT INTO integration_messages
		(id, tenant_id, source_id, message_id, result_id, content_hash, raw_payload, normalized_payload, state, reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'needs_reconciliation','patient-unmatched')
		ON CONFLICT (tenant_id, source_id, message_id) DO NOTHING
		RETURNING id, state, reason, received_at`, id, scope.TenantID, scope.SourceID,
		message.Result.MessageID, message.Result.ResultID, message.ContentHash, message.RawPayload,
		message.NormalizedPayload).Scan(&receipt.ID, &receipt.State, &receipt.Reason, &receipt.ReceivedAt)
	// Unlike ordinary Exec, this INSERT uses QueryRow + RETURNING. DO NOTHING
	// returns no row on a duplicate, so Scan's ErrNoRows is meaningful here.
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash string
		if err := tx.QueryRow(ctx, `SELECT id, state, reason, received_at, content_hash
			FROM integration_messages WHERE tenant_id=$1 AND source_id=$2 AND message_id=$3`,
			scope.TenantID, scope.SourceID, message.Result.MessageID).
			Scan(&receipt.ID, &receipt.State, &receipt.Reason, &receipt.ReceivedAt, &existingHash); err != nil {
			return Receipt{}, false, err
		}
		if existingHash != message.ContentHash {
			return Receipt{}, false, fmt.Errorf("%w: message id reused with different content", internal.ErrConflict)
		}
		return receipt, true, nil // No new work: deferred rollback releases the connection.
	}
	if err != nil {
		return Receipt{}, false, err
	}

	// A new message ID must not sneak a second version of the same clinical
	// result into processing. Lock its scoped result key, then check prior receipts.
	// Different messages for this result serialize; different results can proceed.
	resultKey := strings.Join([]string{scope.TenantID, scope.SourceID, message.Result.ResultID}, "\x1f")
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, resultKey); err != nil {
		return Receipt{}, false, err
	}
	var seenResult bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM integration_messages
		WHERE tenant_id=$1 AND source_id=$2 AND result_id=$3 AND id<>$4)`,
		scope.TenantID, scope.SourceID, message.Result.ResultID, receipt.ID).Scan(&seenResult); err != nil {
		return Receipt{}, false, err
	}
	var patientID, orderKey string
	if seenResult {
		receipt.Reason = "result-already-received"
	} else {
		// Resolve only a unique, explicitly provisioned namespace/value mapping.
		// The parent lock prevents a concurrent patient deletion during this receipt.
		err = tx.QueryRow(ctx, `SELECT p.id FROM integration_patient_mappings m
		JOIN patients p ON p.id=m.patient_id
		WHERE m.tenant_id=$1 AND m.source_id=$2 AND m.identifier_system=$3 AND m.identifier_value=$4
		AND p.deleted_at IS NULL FOR SHARE OF p`, scope.TenantID, scope.SourceID,
			message.Result.PatientIdentifier.System, message.Result.PatientIdentifier.Value).Scan(&patientID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Receipt{}, false, err
		}
		if err == nil {
			receipt.Reason = "order-unmatched-or-patient-mismatch"
			err = tx.QueryRow(ctx, `SELECT order_key FROM integration_order_mappings
			WHERE tenant_id=$1 AND source_id=$2 AND identifier_system=$3 AND identifier_value=$4 AND patient_id=$5`,
				scope.TenantID, scope.SourceID, message.Result.OrderIdentifier.System, message.Result.OrderIdentifier.Value, patientID).Scan(&orderKey)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return Receipt{}, false, err
			}
			if err == nil {
				receipt.State, receipt.Reason = "ready_for_transformation", ""
			}
		}
	}
	// Even ready means only that explicit mappings existed at receipt time.
	// Transformation/delivery must recheck context; no FHIR resource is created here.
	if _, err := tx.Exec(ctx, `UPDATE integration_messages SET state=$2, reason=$3,
		patient_id=NULLIF($4,''), order_key=NULLIF($5,'') WHERE id=$1`,
		receipt.ID, receipt.State, receipt.Reason, patientID, orderKey); err != nil {
		return Receipt{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO integration_events (message_id,event_type)
		VALUES ($1,'received'),($1,$2)`, receipt.ID, receipt.State); err != nil {
		return Receipt{}, false, err
	}
	// This function owns the transaction: only it commits. If the event insert
	// above fails, the deferred rollback also removes the pending receipt.
	if err := tx.Commit(ctx); err != nil {
		return Receipt{}, false, err
	}
	return receipt, false, nil
}

func (s *PostgresStore) Get(ctx context.Context, scope Scope, id string) (Receipt, error) {
	var receipt Receipt
	var observationID, reportID pgtype.Text
	var transformedAt pgtype.Timestamptz
	err := s.pool.QueryRow(ctx, `SELECT id,state,reason,received_at,observation_id,diagnostic_report_id,transformed_at FROM integration_messages
		WHERE id=$1 AND tenant_id=$2 AND source_id=$3`, id, scope.TenantID, scope.SourceID).
		Scan(&receipt.ID, &receipt.State, &receipt.Reason, &receipt.ReceivedAt, &observationID, &reportID, &transformedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, internal.ErrNotFound
	}
	if observationID.Valid {
		receipt.ObservationID = observationID.String
	}
	if reportID.Valid {
		receipt.DiagnosticReportID = reportID.String
	}
	if transformedAt.Valid {
		receipt.TransformedAt = &transformedAt.Time
	}
	return receipt, err
}

// Transform locks the receipt, rechecks that its explicit mappings still point
// to a live patient, then stores the generated FHIR Bundle and state together.
func (s *PostgresStore) Transform(ctx context.Context, scope Scope, id string) (json.RawMessage, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var receiptID, state, reason string
	var patientID, orderKey pgtype.Text
	var normalizedPayload, savedBundle []byte
	var observationID, reportID pgtype.Text
	var transformedAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `SELECT id,state,reason,patient_id,order_key,normalized_payload,
		observation_id,diagnostic_report_id,transformed_at,transformation_bundle
		FROM integration_messages WHERE id=$1 AND tenant_id=$2 AND source_id=$3 FOR UPDATE`,
		id, scope.TenantID, scope.SourceID).Scan(
		&receiptID, &state, &reason, &patientID, &orderKey, &normalizedPayload,
		&observationID, &reportID, &transformedAt, &savedBundle,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, internal.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if state == "transformed" {
		return json.RawMessage(savedBundle), nil
	}
	if state != "ready_for_transformation" {
		return nil, internal.ErrConflict
	}
	if !patientID.Valid || !orderKey.Valid {
		return nil, internal.ErrConflict
	}

	result, err := DecodeResult(normalizedPayload)
	if err != nil {
		return nil, err
	}
	// The mapping may have changed or the Patient may have been soft-deleted
	// since intake. Never build patient-linked FHIR resources from stale context.
	var currentPatient string
	err = tx.QueryRow(ctx, `SELECT p.id FROM patients p
		JOIN integration_patient_mappings pm ON pm.patient_id=p.id
		JOIN integration_order_mappings om ON om.patient_id=p.id
		WHERE p.id=$1 AND p.deleted_at IS NULL
		AND pm.tenant_id=$2 AND pm.source_id=$3
		AND pm.identifier_system=$4 AND pm.identifier_value=$5
		AND om.tenant_id=$2 AND om.source_id=$3
		AND om.identifier_system=$6 AND om.identifier_value=$7 AND om.order_key=$8
		FOR SHARE OF p, pm, om`, patientID.String, scope.TenantID, scope.SourceID,
		result.PatientIdentifier.System, result.PatientIdentifier.Value,
		result.OrderIdentifier.System, result.OrderIdentifier.Value, orderKey.String).Scan(&currentPatient)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, internal.ErrConflict
	}
	if err != nil {
		return nil, err
	}

	issuedAt := time.Now().UTC()
	bundle, observationResourceID, reportResourceID, err := BuildTransactionBundle(scope, currentPatient, result, issuedAt)
	if err != nil {
		return nil, err
	}
	command, err := tx.Exec(ctx, `UPDATE integration_messages
		SET state='transformed', transformation_bundle=$2, observation_id=$3,
			diagnostic_report_id=$4, transformed_at=$5
		WHERE id=$1 AND state='ready_for_transformation'`,
		receiptID, []byte(bundle), observationResourceID, reportResourceID, issuedAt)
	if err != nil {
		return nil, err
	}
	if command.RowsAffected() != 1 {
		return nil, internal.ErrConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO integration_events (message_id,event_type) VALUES ($1,'transformed')`, receiptID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return bundle, nil
}
