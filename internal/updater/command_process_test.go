package updater

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitForCommandExitTimesOutAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exitResult := make(chan error, 1)
	terminated := make(chan struct{}, 1)
	err := waitForCommandExitWithTimeout(ctx, func() error {
		return <-exitResult
	}, func() {
		terminated <- struct{}{}
	}, 10*time.Millisecond)

	var timeoutErr *commandLifecycleTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected typed lifecycle timeout, got %v", err)
	}
	if timeoutErr.Phase != "process exit after cancellation" {
		t.Fatalf("unexpected timeout phase: %#v", timeoutErr)
	}
	select {
	case <-terminated:
	case <-time.After(time.Second):
		t.Fatal("expected process termination attempt")
	}
	exitResult <- nil
}

func TestWaitForOutputDrainTimesOutAndClosesPipes(t *testing.T) {
	drainDone := make(chan struct{})
	pipesClosed := make(chan struct{}, 1)
	err := waitForOutputDrainWithTimeout(drainDone, func() {
		pipesClosed <- struct{}{}
	}, context.Canceled, 10*time.Millisecond)

	var timeoutErr *commandLifecycleTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected typed lifecycle timeout, got %v", err)
	}
	if timeoutErr.Phase != "output drain after cancellation" {
		t.Fatalf("unexpected timeout phase: %#v", timeoutErr)
	}
	select {
	case <-pipesClosed:
	case <-time.After(time.Second):
		t.Fatal("expected pipe close attempt")
	}
	close(drainDone)
}
