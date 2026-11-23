package integration

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WorkerConfig holds tuning knobs for the background delivery worker.
type WorkerConfig struct {
	PollInterval time.Duration
	BatchSize    int
}

// Worker autonomously polls the database for messages ready for transformation
// and pending outbox delivery jobs, delivering them reliably without manual HTTP triggers.
type Worker struct {
	pool      *pgxpool.Pool
	service   *Service
	cfg       WorkerConfig
	triggerCh chan struct{}
	stopCh    chan struct{}
	wg        sync.WaitGroup
	mu        sync.Mutex
	running   bool
}

// NewWorker initializes a background worker with configurable polling.
func NewWorker(pool *pgxpool.Pool, service *Service, cfg ...WorkerConfig) *Worker {
	c := WorkerConfig{
		PollInterval: 1 * time.Second,
		BatchSize:    10,
	}
	if len(cfg) > 0 {
		if cfg[0].PollInterval > 0 {
			c.PollInterval = cfg[0].PollInterval
		}
		if cfg[0].BatchSize > 0 {
			c.BatchSize = cfg[0].BatchSize
		}
	}
	return &Worker{
		pool:      pool,
		service:   service,
		cfg:       c,
		triggerCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
	}
}

// Trigger nudges the worker loop to process immediately without waiting for the next ticker tick.
func (w *Worker) Trigger() {
	select {
	case w.triggerCh <- struct{}{}:
	default:
	}
}

// Start launches the background worker loop in a separate goroutine.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopCh:
				return
			case <-ticker.C:
				w.ProcessOnce(ctx)
			case <-w.triggerCh:
				w.ProcessOnce(ctx)
			}
		}
	}()
}

// Stop signals the worker to terminate and waits for in-flight tasks to finish.
func (w *Worker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	close(w.stopCh)
	w.running = false
	w.mu.Unlock()
	w.wg.Wait()
}

// ProcessOnce runs a single pass of transformations and deliveries.
// Publicly exposed for deterministic execution in unit tests.
func (w *Worker) ProcessOnce(ctx context.Context) {
	w.processTransformations(ctx)
	w.processDeliveries(ctx)
}

func (w *Worker) processTransformations(ctx context.Context) {
	if w.pool == nil {
		return
	}
	rows, err := w.pool.Query(ctx, `
		SELECT id, tenant_id, source_id
		FROM integration_messages
		WHERE state = 'ready_for_transformation'
		ORDER BY received_at ASC
		LIMIT $1
	`, w.cfg.BatchSize)
	if err != nil {
		return
	}
	defer rows.Close()

	type msgTarget struct {
		id, tenantID, sourceID string
	}
	var targets []msgTarget
	for rows.Next() {
		var t msgTarget
		if err := rows.Scan(&t.id, &t.tenantID, &t.sourceID); err == nil {
			targets = append(targets, t)
		}
	}
	rows.Close()

	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		scope := Scope{TenantID: t.tenantID, SourceID: t.sourceID}
		if _, err := w.service.Transform(ctx, scope, t.id); err != nil {
			log.Printf("[worker] transform error for message %s: %v", t.id, err)
		} else {
			log.Printf("[worker] auto-transformed message %s into FHIR bundle", t.id)
		}
	}
}

func (w *Worker) processDeliveries(ctx context.Context) {
	if w.pool == nil {
		return
	}
	rows, err := w.pool.Query(ctx, `
		SELECT message_id, tenant_id, source_id, destination
		FROM integration_delivery_jobs
		WHERE state = 'pending'
		  AND next_attempt_at <= clock_timestamp()
		  AND attempt_count < max_attempts
		ORDER BY next_attempt_at ASC
		LIMIT $1
	`, w.cfg.BatchSize)
	if err != nil {
		return
	}
	defer rows.Close()

	type jobTarget struct {
		messageID, tenantID, sourceID, destination string
	}
	var targets []jobTarget
	for rows.Next() {
		var t jobTarget
		if err := rows.Scan(&t.messageID, &t.tenantID, &t.sourceID, &t.destination); err == nil {
			targets = append(targets, t)
		}
	}
	rows.Close()

	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		scope := Scope{TenantID: t.tenantID, SourceID: t.sourceID}
		res, err := w.service.Deliver(ctx, scope, t.messageID, t.destination)
		if err != nil {
			log.Printf("[worker] deliver error for message %s to %s: %v", t.messageID, t.destination, err)
		} else {
			log.Printf("[worker] delivered message %s to %s (status %d)", t.messageID, t.destination, res.StatusCode)
		}
	}
}
