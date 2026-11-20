package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go-fhir-phase1/internal"
	"go-fhir-phase1/internal/migrations"
	"go-fhir-phase1/internal/repository"
)

// Each integration test owns a disposable schema; application .env is never read.
func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"intake_" + strings.ToLower(rand.Text())}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close(ctx)
	})
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 6
	pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	patients := repository.NewPatientRepository(pool)
	if err := patients.InitTable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatalf("migration not repeatable: %v", err)
	}
	for _, id := range []string{"p1", "p2"} {
		p := internal.Patient{ResourceType: "Patient", ID: id, Meta: internal.Meta{VersionID: "1"}}
		data, _ := json.Marshal(p)
		if err := patients.SavePatient(ctx, p, data); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func installMappings(t *testing.T, pool *pgxpool.Pool, orderPatient string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO integration_patient_mappings VALUES
		('synthetic-demo','synthetic-lis','https://example.org/synthetic-lis/patients','00001','p1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO integration_order_mappings VALUES
		('synthetic-demo','synthetic-lis','https://example.org/synthetic-lis/orders','SYN-ORDER-1',$1,'known-synthetic-order')`, orderPatient); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresIntakeIdentityAndIsolation(t *testing.T) {
	pool := database(t)
	service := NewService(NewPostgresStore(pool))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	scope := Scope{"synthetic-demo", "synthetic-lis"}
	raw := sample(t)
	unmatched, _, err := service.Submit(ctx, scope, raw)
	if err != nil || unmatched.State != "needs_reconciliation" || unmatched.Reason != "patient-unmatched" {
		t.Fatalf("unmatched: %+v %v", unmatched, err)
	}
	installMappings(t, pool, "p1")
	raw2 := bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("demo-message-1"), []byte("demo-message-2")), []byte("demo-result-1"), []byte("demo-result-2"))
	ready, _, err := service.Submit(ctx, scope, raw2)
	if err != nil || ready.State != "ready_for_transformation" {
		t.Fatalf("matched: %+v %v", ready, err)
	}
	// A new store instance can retrieve the committed receipt; no in-memory queue
	// or process-local map is needed to retain accepted work.
	if got, err := NewPostgresStore(pool).Get(ctx, scope, ready.ID); err != nil || got.ID != ready.ID {
		t.Fatal("receipt not durable")
	}
	for _, other := range []Scope{{"other-tenant", "synthetic-lis"}, {"synthetic-demo", "other-source"}} {
		if _, err := service.Get(ctx, other, ready.ID); !errors.Is(err, internal.ErrNotFound) {
			t.Fatal("receipt crossed scope boundary")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE integration_order_mappings SET patient_id='p2'`); err != nil {
		t.Fatal(err)
	}
	raw3 := bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("demo-message-1"), []byte("demo-message-3")), []byte("demo-result-1"), []byte("demo-result-3"))
	blocked, _, err := service.Submit(ctx, scope, raw3)
	if err != nil || blocked.State != "needs_reconciliation" || blocked.Reason != "order-unmatched-or-patient-mismatch" {
		t.Fatalf("mismatched order accepted: %+v %v", blocked, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE patients SET deleted_at=clock_timestamp() WHERE id='p1'`); err != nil {
		t.Fatal(err)
	}
	raw4 := bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("demo-message-1"), []byte("demo-message-4")), []byte("demo-result-1"), []byte("demo-result-4"))
	deleted, _, err := service.Submit(ctx, scope, raw4)
	if err != nil || deleted.Reason != "patient-unmatched" {
		t.Fatal("deleted patient matched")
	}
}

func TestPostgresIntakeConcurrentDeduplicationAndRollback(t *testing.T) {
	pool := database(t)
	installMappings(t, pool, "p1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	service, scope, raw := NewService(NewPostgresStore(pool)), Scope{"synthetic-demo", "synthetic-lis"}, sample(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, duplicate, err := service.Submit(ctx, scope, raw)
			if err != nil {
				t.Error(err)
				return
			}
			if !duplicate {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("created %d receipts for one message", created)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_events`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate audit entries: %d %v", count, err)
	}
	var savedRaw []byte
	if err := pool.QueryRow(ctx, `SELECT raw_payload FROM integration_messages`).Scan(&savedRaw); err != nil || !bytes.Equal(raw, savedRaw) {
		t.Fatal("original message not preserved")
	}
	if _, _, err := service.Submit(ctx, scope, bytes.Replace(raw, []byte("5.20"), []byte("5.21"), 1)); !errors.Is(err, internal.ErrConflict) {
		t.Fatal("message content collision accepted")
	}
	newID := bytes.Replace(raw, []byte("demo-message-1"), []byte("different-message"), 1)
	blocked, _, err := service.Submit(ctx, scope, newID)
	if err != nil || blocked.Reason != "result-already-received" {
		t.Fatal("same result under a new message id was not blocked")
	}
	// Deliberately break the event write. The receipt insert must roll back too.
	if _, err := pool.Exec(ctx, `ALTER TABLE integration_events ADD CONSTRAINT reject_receipt CHECK(event_type<>'received') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	failed := bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("demo-message-1"), []byte("will-rollback")), []byte("demo-result-1"), []byte("rollback-result"))
	if _, _, err := service.Submit(ctx, scope, failed); err == nil {
		t.Fatal("broken audit write succeeded")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_messages WHERE message_id='will-rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed transaction left a receipt")
	}
	if _, err := pool.Exec(ctx, `UPDATE integration_schema_migrations SET checksum='tampered'`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, pool); err == nil {
		t.Fatal("changed applied migration was ignored")
	}
}
