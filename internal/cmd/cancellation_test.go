// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotandev/glassbox/internal/simulator"
	"github.com/dotandev/glassbox/internal/shutdown"
)

// ── MockRunner behaviour under cancellation ───────────────────────────────────

// blockingRunner is a RunnerInterface whose Run blocks until the context is
// cancelled, simulating a long replay that is interrupted mid-flight.
type blockingRunner struct {
	closed bool
}

func (r *blockingRunner) Run(ctx context.Context, req *simulator.SimulationRequest) (*simulator.SimulationResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *blockingRunner) Close() error {
	r.closed = true
	return nil
}

// TestRunnerClose_CalledOnCancel verifies that Close() is called when the
// context is cancelled while runner.Run is blocked.  This mirrors the
// `defer runner.Close()` added to debug.go RunE.
func TestRunnerClose_CalledOnCancel(t *testing.T) {
	runner := &blockingRunner{}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		// Simulate the minimal work debug.go does: call Run then defer Close.
		defer runner.Close()
		_, runErr := runner.Run(ctx, &simulator.SimulationRequest{})
		done <- runErr
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner.Run did not return after cancel")
	}

	// Give the defer a moment to fire.
	time.Sleep(10 * time.Millisecond)
	if !runner.closed {
		t.Error("runner.Close() should have been called via defer after cancellation")
	}
}

// ── Shutdown coordinator closes runner on interrupt ───────────────────────────

// TestRegisterRunnerCloseHook_RunsOnShutdown verifies that
// registerRunnerCloseHook wires a hook that calls runner.Close during the
// shutdown coordinator's Run.
func TestRegisterRunnerCloseHook_RunsOnShutdown(t *testing.T) {
	coord := shutdown.NewCoordinator()
	setShutdownCoordinator(coord)
	defer clearShutdownCoordinator()

	runner := simulator.NewMockRunner(nil)
	closeCalled := false
	runner.CloseFunc = func() error {
		closeCalled = true
		return nil
	}

	registerRunnerCloseHook("test-runner", runner)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := coord.Run(ctx); err != nil {
		t.Fatalf("coordinator.Run: %v", err)
	}

	if !closeCalled {
		t.Error("runner.Close() should have been called by the shutdown hook")
	}
}

// TestRegisterRunnerCloseHook_NilRunner_NoOp verifies that passing a nil
// runner to registerRunnerCloseHook is safe (no panic, no hook registered).
func TestRegisterRunnerCloseHook_NilRunner_NoOp(t *testing.T) {
	coord := shutdown.NewCoordinator()
	setShutdownCoordinator(coord)
	defer clearShutdownCoordinator()

	// Must not panic.
	registerRunnerCloseHook("nil-runner", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := coord.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── Cancellation is distinct from generic failure ─────────────────────────────

// TestIsCancellation_Vs_GenericError verifies that context errors are
// classified as cancellation, not as generic internal failures, so the exit
// code is 130 (interrupt) rather than 3 (internal error).
func TestIsCancellation_Vs_GenericError(t *testing.T) {
	if !IsCancellation(context.Canceled) {
		t.Error("context.Canceled should be recognised as cancellation")
	}
	if !IsCancellation(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should be recognised as cancellation")
	}
	if IsCancellation(errors.New("some other error")) {
		t.Error("a generic error should NOT be recognised as cancellation")
	}
	if IsCancellation(nil) {
		t.Error("nil should NOT be recognised as cancellation")
	}
}

func TestIsInterrupted_VsContextCanceled(t *testing.T) {
	// ErrInterrupted is the signal-level interrupt sentinel; context.Canceled
	// is the programmatic cancellation. They must be distinct so that
	// IsInterrupted doesn't accidentally match every cancelled operation.
	if IsInterrupted(context.Canceled) {
		t.Error("context.Canceled should NOT match IsInterrupted; use IsCancellation instead")
	}
	if !IsInterrupted(ErrInterrupted) {
		t.Error("ErrInterrupted must match IsInterrupted")
	}
}

// ── Partial-output cleanup ────────────────────────────────────────────────────

// TestRemoveIfCancelled_RemovesFileOnCancel verifies that removeIfCancelled
// deletes the named file when the context has been cancelled.
func TestRemoveIfCancelled_RemovesFileOnCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.json")

	// Create a partial output file.
	if err := os.WriteFile(path, []byte(`{"partial":true}`), 0o644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	removeIfCancelled(ctx, path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("partial file should have been removed on cancel; Stat returned: %v", err)
	}
}

// TestRemoveIfCancelled_KeepsFileWhenNotCancelled verifies that
// removeIfCancelled leaves the file untouched when the context is still alive.
func TestRemoveIfCancelled_KeepsFileWhenNotCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.json")

	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := context.Background() // never cancelled

	removeIfCancelled(ctx, path)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should still exist when context is not cancelled; Stat: %v", err)
	}
}

// TestRemoveIfCancelled_EmptyPath_NoOp verifies that an empty path does not
// panic or return an error.
func TestRemoveIfCancelled_EmptyPath_NoOp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Must not panic.
	removeIfCancelled(ctx, "")
}

// TestRemoveIfCancelled_NonexistentFile_NoOp verifies that trying to remove a
// file that doesn't exist (already cleaned up) is safe.
func TestRemoveIfCancelled_NonexistentFile_NoOp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Must not panic even if the file doesn't exist.
	removeIfCancelled(ctx, filepath.Join(t.TempDir(), "does-not-exist.json"))
}

// ── backupFilePath (from config_migrate.go) ───────────────────────────────────

// TestBackupFilePath_Format verifies that backupFilePath produces a path of
// the form <original>.<timestamp>.bak alongside the original file.
func TestBackupFilePath_Format(t *testing.T) {
	orig := "/home/user/.glassbox/config.toml"
	bak := backupFilePath(orig)

	if bak[:len(orig)] != orig {
		t.Errorf("backup path should start with original path %q, got %q", orig, bak)
	}
	if bak[len(orig)] != '.' {
		t.Errorf("expected '.' separator after original path, got %q", string(bak[len(orig)]))
	}
	if bak[len(bak)-4:] != ".bak" {
		t.Errorf("backup path should end with .bak, got %q", bak)
	}
}
