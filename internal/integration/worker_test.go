package integration

import (
	"context"
	"testing"
	"time"
)

func TestWorkerStartStopWithContext(t *testing.T) {
	service := NewService(&captureStore{})
	worker := NewWorker(nil, service, WorkerConfig{PollInterval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	// Send triggers while running to ensure no deadlock
	for i := 0; i < 5; i++ {
		worker.Trigger()
	}

	time.Sleep(30 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		worker.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("worker did not stop cleanly on context cancellation")
	}
}

func TestWorkerStopExplicit(t *testing.T) {
	service := NewService(&captureStore{})
	worker := NewWorker(nil, service, WorkerConfig{PollInterval: 10 * time.Millisecond})

	worker.Start(context.Background())

	worker.Trigger()
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		worker.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("worker did not stop cleanly on Stop()")
	}
}

func TestWorkerDoubleStartStop(t *testing.T) {
	service := NewService(&captureStore{})
	worker := NewWorker(nil, service, WorkerConfig{PollInterval: 10 * time.Millisecond})

	ctx := context.Background()
	worker.Start(ctx)
	worker.Start(ctx) // Idempotent

	worker.Stop()
	worker.Stop() // Idempotent
}
