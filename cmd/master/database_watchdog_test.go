package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDatabaseWatchdogExitsAfterConsecutiveFailures(t *testing.T) {
	w := &databaseWatchdog{fatal: make(chan error, 1)}
	w.ready.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx, time.Millisecond, 10*time.Millisecond, 3,
		func(context.Context) error { return errors.New("database unavailable") }, nil)

	select {
	case err := <-w.Fatal():
		if err == nil {
			t.Fatal("fatal error is nil")
		}
		if w.Ready() {
			t.Fatal("watchdog remained ready after fatal failure")
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not report fatal database failure")
	}
}

func TestDatabaseWatchdogRequiresConsecutiveFailures(t *testing.T) {
	w := &databaseWatchdog{fatal: make(chan error, 1)}
	w.ready.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go w.run(ctx, time.Millisecond, 10*time.Millisecond, 3, func(context.Context) error {
		calls++
		if calls%3 == 0 {
			return nil
		}
		return errors.New("temporary failure")
	}, nil)
	time.Sleep(15 * time.Millisecond)
	cancel()

	select {
	case err := <-w.Fatal():
		t.Fatalf("watchdog reported non-consecutive failures as fatal: %v", err)
	default:
	}
	if !w.Ready() {
		t.Fatal("watchdog marked database unready after recovered failures")
	}
}

func TestDatabaseWatchdogFailIsIdempotent(t *testing.T) {
	w := &databaseWatchdog{fatal: make(chan error, 1)}
	w.ready.Store(true)
	first := errors.New("first")
	w.Fail(first)
	w.Fail(errors.New("second"))

	if w.Ready() {
		t.Fatal("watchdog remained ready after Fail")
	}
	select {
	case err := <-w.Fatal():
		if !errors.Is(err, first) {
			t.Fatalf("fatal error = %v, want first error", err)
		}
	default:
		t.Fatal("watchdog did not report fatal error")
	}
}
