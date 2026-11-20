package repository

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"go-fhir-phase1/internal"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"test_fhir_" + strings.ToLower(rand.Text())}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("cleanup isolated schema: %v", err)
		}
		admin.Close(cleanupCtx)
	})
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 4
	pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	for _, init := range []func(context.Context) error{NewPatientRepository(pool).InitTable, NewObservationRepository(pool).InitTable, NewConditionRepository(pool).InitTable} {
		if err := init(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func resourceJSON(t *testing.T, resource any) []byte {
	t.Helper()
	data, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPostgresResourceIntegrity(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	patients, observations, conditions := NewPatientRepository(pool), NewObservationRepository(pool), NewConditionRepository(pool)
	p := internal.Patient{ResourceType: "Patient", ID: "p1", Meta: internal.Meta{VersionID: "1"}, Identifier: []internal.Identifier{{System: "urn:test", Value: "record-1"}}, Name: []internal.HumanName{{Family: "Example"}}}
	if err := patients.SavePatient(ctx, p, resourceJSON(t, p)); err != nil {
		t.Fatal(err)
	}
	if err := patients.SavePatient(ctx, p, resourceJSON(t, p)); !errors.Is(err, internal.ErrConflict) {
		t.Fatalf("duplicate overwrote patient: %v", err)
	}
	for _, term := range []struct {
		search string
		count  int
	}{{"Example", 1}, {"Absent", 0}} {
		data, err := patients.SearchPatients(ctx, "record-1", term.search)
		if err != nil || len(data) != term.count {
			t.Fatalf("combined search count=%d err=%v", len(data), err)
		}
	}
	p.Meta.VersionID = "2"
	if err := patients.UpdatePatient(ctx, p, resourceJSON(t, p), "1"); err != nil {
		t.Fatal(err)
	}
	if err := patients.UpdatePatient(ctx, p, resourceJSON(t, p), "1"); !errors.Is(err, internal.ErrPrecondition) {
		t.Fatalf("stale update accepted: %v", err)
	}
	o := internal.Observation{ResourceType: "Observation", ID: "o1", Meta: internal.Meta{VersionID: "1"}, Status: "final", Code: internal.CodeableConcept{Text: "test"}, Subject: internal.Reference{Reference: "Patient/missing"}}
	if err := observations.SaveObservation(ctx, o, resourceJSON(t, o)); !errors.Is(err, internal.ErrInvalid) {
		t.Fatalf("missing patient accepted: %v", err)
	}
	o.Subject.Reference = "Patient/p1"
	if err := observations.SaveObservation(ctx, o, resourceJSON(t, o)); err != nil {
		t.Fatal(err)
	}
	if err := observations.SaveObservation(ctx, o, resourceJSON(t, o)); !errors.Is(err, internal.ErrConflict) {
		t.Fatalf("duplicate overwrote observation: %v", err)
	}
	if err := patients.DeletePatient(ctx, "p1", "1"); !errors.Is(err, internal.ErrPrecondition) {
		t.Fatalf("stale delete accepted: %v", err)
	}
	rows, err := observations.GetObservationsByPatient(ctx, "p1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("observation search count=%d err=%v", len(rows), err)
	}
	c := internal.Condition{ResourceType: "Condition", ID: "c1", Meta: internal.Meta{VersionID: "1"}, Code: internal.CodeableConcept{Text: "test"}, Subject: internal.Reference{Reference: "Patient/missing"}}
	if err := conditions.SaveCondition(ctx, c, resourceJSON(t, c)); !errors.Is(err, internal.ErrInvalid) {
		t.Fatalf("missing condition patient accepted: %v", err)
	}
	c.Subject.Reference = "Patient/p1"
	if err := conditions.SaveCondition(ctx, c, resourceJSON(t, c)); err != nil {
		t.Fatal(err)
	}
	rows, err = conditions.GetConditionsByPatient(ctx, "Patient/p1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("condition search count=%d err=%v", len(rows), err)
	}
	if err := patients.DeletePatient(ctx, "p1", "2"); err != nil {
		t.Fatal(err)
	}
	if err := patients.DeletePatient(ctx, "p1", ""); err != nil {
		t.Fatalf("repeated delete: %v", err)
	}
	if _, err := patients.GetPatientByID(ctx, "p1"); !errors.Is(err, internal.ErrGone) {
		t.Fatalf("deleted patient read: %v", err)
	}
	if rows, err := patients.SearchPatients(ctx, "", ""); err != nil || len(rows) != 0 {
		t.Fatalf("deleted patient in search: rows=%d err=%v", len(rows), err)
	}
	versions, err := patients.GetPatientHistory(ctx, "p1")
	if err != nil || len(versions) != 3 || versions[0].Method != "DELETE" || versions[0].VersionID != "3" || len(versions[0].Data) != 0 {
		t.Fatalf("history after deletion: %+v err=%v", versions, err)
	}
	if _, err := patients.GetPatientVersion(ctx, "p1", "1"); err != nil {
		t.Fatalf("old version missing: %v", err)
	}
	if _, err := observations.GetObservationByID(ctx, "o1"); !errors.Is(err, internal.ErrGone) {
		t.Fatalf("cascade left observation live: %v", err)
	}
	if _, err := conditions.GetConditionByID(ctx, "c1"); !errors.Is(err, internal.ErrGone) {
		t.Fatalf("cascade left condition live: %v", err)
	}
	if rows, err := observations.GetObservationsByPatient(ctx, "p1"); err != nil || len(rows) != 0 {
		t.Fatalf("cascade observation search: %d %v", len(rows), err)
	}
	if rows, err := conditions.GetConditionsByPatient(ctx, "Patient/p1"); err != nil || len(rows) != 0 {
		t.Fatalf("cascade condition search: %d %v", len(rows), err)
	}
	oHistory, err := observations.GetObservationHistory(ctx, "o1")
	if err != nil || len(oHistory) != 2 || oHistory[0].Method != "DELETE" {
		t.Fatalf("observation history: %+v %v", oHistory, err)
	}
	cHistory, err := conditions.GetConditionHistory(ctx, "c1")
	if err != nil || len(cHistory) != 2 || cHistory[0].Method != "DELETE" {
		t.Fatalf("condition history: %+v %v", cHistory, err)
	}
	if _, err := observations.GetObservationVersion(ctx, "o1", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := conditions.GetConditionVersion(ctx, "c1", "1"); err != nil {
		t.Fatal(err)
	}
	if err := patients.UpdatePatient(ctx, p, resourceJSON(t, p), "2"); !errors.Is(err, internal.ErrGone) {
		t.Fatalf("updated deleted patient: %v", err)
	}
	o.ID = "o2"
	if err := observations.SaveObservation(ctx, o, resourceJSON(t, o)); !errors.Is(err, internal.ErrInvalid) {
		t.Fatalf("observation accepted deleted patient: %v", err)
	}
	c.ID = "c2"
	if err := conditions.SaveCondition(ctx, c, resourceJSON(t, c)); !errors.Is(err, internal.ErrInvalid) {
		t.Fatalf("condition accepted deleted patient: %v", err)
	}
	var retained bool
	if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM patients WHERE id = 'p1'`).Scan(&retained); err != nil || !retained {
		t.Fatalf("row not retained: %v", err)
	}
}

func TestPostgresConcurrentDeletionRejectsClinicalInsert(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := internal.Patient{ResourceType: "Patient", ID: "p1"}
	if err := NewPatientRepository(pool).SavePatient(ctx, p, resourceJSON(t, p)); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "UPDATE patients SET deleted_at = clock_timestamp() WHERE id = 'p1'"); err != nil {
		t.Fatal(err)
	}
	o := internal.Observation{ResourceType: "Observation", ID: "o1", Subject: internal.Reference{Reference: "Patient/p1"}}
	data := resourceJSON(t, o)
	finished := make(chan error, 1)
	go func() { finished <- NewObservationRepository(pool).SaveObservation(ctx, o, data) }()
	// Wait until PostgreSQL confirms the insert is blocked on the deleting transaction.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND datname = current_database() AND query LIKE 'INSERT INTO observations%' AND wait_event_type = 'Lock')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-finished:
			t.Fatalf("insert did not wait for patient deletion: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, internal.ErrInvalid) {
		t.Fatalf("concurrent insertion left dangling reference: %v", err)
	}
}

func TestPostgresHistoryMigrationAndWriteRollback(t *testing.T) {
	pool := integrationPool(t)
	patients := NewPatientRepository(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Simulate an existing Patient from before history support was installed.
	legacy := []byte(`{"resourceType":"Patient","id":"legacy-patient","meta":{"versionId":"7"},"extra":{"large":9007199254740993}}`)
	if _, err := pool.Exec(ctx, `INSERT INTO patients (id, resource_type, data) VALUES ('legacy-patient', 'Patient', $1)`, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO patients (id, resource_type, data) VALUES ('unversioned', 'Patient', '{"resourceType":"Patient","id":"unversioned"}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := patients.InitTable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	history, err := patients.GetPatientHistory(ctx, "legacy-patient")
	if err != nil || len(history) != 1 || history[0].VersionID != "7" {
		t.Fatalf("migration history: %+v %v", history, err)
	}
	var preserved string
	if err := pool.QueryRow(ctx, `SELECT data->'extra'->>'large' FROM patient_history WHERE patient_id = 'legacy-patient'`).Scan(&preserved); err != nil || preserved != "9007199254740993" {
		t.Fatalf("legacy data changed: %s %v", preserved, err)
	}
	if _, err := patients.GetPatientVersion(ctx, "unversioned", "legacy"); err != nil {
		t.Fatalf("unversioned snapshot missing: %v", err)
	}
	p := internal.Patient{ResourceType: "Patient", ID: "p1", Meta: internal.Meta{VersionID: "1"}}
	if err := patients.SavePatient(ctx, p, resourceJSON(t, p)); err != nil {
		t.Fatal(err)
	}
	// A history-write failure must roll back the current-row update too.
	p.Meta.VersionID = "2"
	if _, err := pool.Exec(ctx, `INSERT INTO patient_history (patient_id, version_id, data, method) VALUES ('p1', '2', $1, 'PUT')`, resourceJSON(t, p)); err != nil {
		t.Fatal(err)
	}
	if err := patients.UpdatePatient(ctx, p, resourceJSON(t, p), "1"); !errors.Is(err, internal.ErrConflict) {
		t.Fatalf("expected snapshot-write conflict: %v", err)
	}
	data, err := patients.GetPatientByID(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	var current internal.Patient
	if err := json.Unmarshal(data, &current); err != nil {
		t.Fatal(err)
	}
	if current.Meta.VersionID != "1" {
		t.Fatal("failed history write did not roll back update")
	}
	// The same atomicity rule applies when creation's snapshot cannot be saved.
	if _, err := pool.Exec(ctx, `ALTER TABLE patient_history ADD CONSTRAINT reject_test_create CHECK (patient_id <> 'fail-create')`); err != nil {
		t.Fatal(err)
	}
	p.ID, p.Meta.VersionID = "fail-create", "1"
	if err := patients.SavePatient(ctx, p, resourceJSON(t, p)); err == nil {
		t.Fatal("expected forced snapshot failure")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patients WHERE id = 'fail-create')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("failed history write left a created patient without a snapshot")
	}
}

func TestPostgresClinicalUpdatesAndIndividualDeletion(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	patients := NewPatientRepository(pool)
	p := internal.Patient{ResourceType: "Patient", ID: "p1", Meta: internal.Meta{VersionID: "1"}}
	if err := patients.SavePatient(ctx, p, resourceJSON(t, p)); err != nil {
		t.Fatal(err)
	}
	observations, conditions := NewObservationRepository(pool), NewConditionRepository(pool)
	o := internal.Observation{ResourceType: "Observation", ID: "o1", Meta: internal.Meta{VersionID: "1"}, Status: "final", Code: internal.CodeableConcept{Text: "original"}, Subject: internal.Reference{Reference: "Patient/p1"}}
	c := internal.Condition{ResourceType: "Condition", ID: "c1", Meta: internal.Meta{VersionID: "1"}, Code: internal.CodeableConcept{Text: "original"}, Subject: internal.Reference{Reference: "Patient/p1"}}
	if err := observations.SaveObservation(ctx, o, resourceJSON(t, o)); err != nil {
		t.Fatal(err)
	}
	if err := conditions.SaveCondition(ctx, c, resourceJSON(t, c)); err != nil {
		t.Fatal(err)
	}
	o.Meta.VersionID, o.Code.Text = "2", "updated"
	c.Meta.VersionID, c.Code.Text = "2", "updated"
	if err := observations.UpdateObservation(ctx, o, resourceJSON(t, o), "1"); err != nil {
		t.Fatal(err)
	}
	if err := conditions.UpdateCondition(ctx, c, resourceJSON(t, c), "1"); err != nil {
		t.Fatal(err)
	}
	if err := observations.UpdateObservation(ctx, o, resourceJSON(t, o), "1"); !errors.Is(err, internal.ErrPrecondition) {
		t.Fatalf("stale observation write: %v", err)
	}
	if err := conditions.UpdateCondition(ctx, c, resourceJSON(t, c), "1"); !errors.Is(err, internal.ErrPrecondition) {
		t.Fatalf("stale condition write: %v", err)
	}
	if err := observations.DeleteObservation(ctx, "o1", "1"); !errors.Is(err, internal.ErrPrecondition) {
		t.Fatalf("stale observation delete: %v", err)
	}
	if err := observations.DeleteObservation(ctx, "o1", "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := patients.GetPatientByID(ctx, "p1"); err != nil {
		t.Fatal("individual deletion hid the Patient")
	}
	if _, err := conditions.GetConditionByID(ctx, "c1"); err != nil {
		t.Fatal("individual deletion hid another resource")
	}
	if err := patients.DeletePatient(ctx, "p1", ""); err != nil {
		t.Fatal(err)
	}
	old, err := observations.GetObservationVersion(ctx, "o1", "1")
	if err != nil {
		t.Fatal(err)
	}
	var original internal.Observation
	if err := json.Unmarshal(old.Data, &original); err != nil || original.Code.Text != "original" {
		t.Fatal("observation snapshot was overwritten")
	}
	oHistory, err := observations.GetObservationHistory(ctx, "o1")
	if err != nil || len(oHistory) != 3 || oHistory[0].VersionID != "3" {
		t.Fatalf("duplicate cascade deletion: %+v %v", oHistory, err)
	}
	cHistory, err := conditions.GetConditionHistory(ctx, "c1")
	if err != nil || len(cHistory) != 3 || cHistory[0].Method != "DELETE" {
		t.Fatalf("condition cascade history: %+v %v", cHistory, err)
	}
}

func TestPostgresCascadeFailureRollsBackAllResources(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	patients, observations, conditions := NewPatientRepository(pool), NewObservationRepository(pool), NewConditionRepository(pool)
	for _, id := range []string{"p1", "p2"} {
		p := internal.Patient{ResourceType: "Patient", ID: id, Meta: internal.Meta{VersionID: "1"}}
		if err := patients.SavePatient(ctx, p, resourceJSON(t, p)); err != nil {
			t.Fatal(err)
		}
		o := internal.Observation{ResourceType: "Observation", ID: "o-" + id, Meta: internal.Meta{VersionID: "1"}, Subject: internal.Reference{Reference: "Patient/" + id}}
		c := internal.Condition{ResourceType: "Condition", ID: "c-" + id, Meta: internal.Meta{VersionID: "1"}, Subject: internal.Reference{Reference: "Patient/" + id}}
		if err := observations.SaveObservation(ctx, o, resourceJSON(t, o)); err != nil {
			t.Fatal(err)
		}
		if err := conditions.SaveCondition(ctx, c, resourceJSON(t, c)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE condition_history ADD CONSTRAINT reject_delete_marker CHECK (method <> 'DELETE')`); err != nil {
		t.Fatal(err)
	}
	if err := patients.DeletePatient(ctx, "p1", ""); err == nil {
		t.Fatal("expected cascade snapshot failure")
	}
	if _, err := patients.GetPatientByID(ctx, "p1"); err != nil {
		t.Fatal("failed cascade hid Patient")
	}
	if _, err := observations.GetObservationByID(ctx, "o-p1"); err != nil {
		t.Fatal("failed cascade hid Observation")
	}
	if _, err := conditions.GetConditionByID(ctx, "c-p1"); err != nil {
		t.Fatal("failed cascade hid Condition")
	}
	history, err := observations.GetObservationHistory(ctx, "o-p1")
	if err != nil || len(history) != 1 {
		t.Fatalf("failed cascade left extra versions: %+v %v", history, err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE condition_history DROP CONSTRAINT reject_delete_marker`); err != nil {
		t.Fatal(err)
	}
	if err := patients.DeletePatient(ctx, "p1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := patients.GetPatientByID(ctx, "p2"); err != nil {
		t.Fatal("cascade touched another Patient")
	}
	if _, err := observations.GetObservationByID(ctx, "o-p2"); err != nil {
		t.Fatal("cascade touched another Patient's Observation")
	}
	if _, err := conditions.GetConditionByID(ctx, "c-p2"); err != nil {
		t.Fatal("cascade touched another Patient's Condition")
	}
}
