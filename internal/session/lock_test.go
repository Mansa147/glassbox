// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// ── AcquireLock / Release ─────────────────────────────────────────────────────

func TestAcquireLock_FirstCallerSucceeds(t *testing.T) {
	t.Parallel()
	id := "lock-test-" + GenerateID("tx1")
	h, err := AcquireLock(id)
	if err != nil {
		t.Fatalf("expected no error acquiring fresh lock, got: %v", err)
	}
	defer h.Release()
}

func TestAcquireLock_SecondCallerBlocked(t *testing.T) {
	t.Parallel()
	id := "lock-held-" + GenerateID("tx2")
	h1, err := AcquireLock(id)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	defer h1.Release()

	_, err2 := AcquireLock(id)
	if err2 == nil {
		t.Fatal("expected error when lock already held, got nil")
	}
	if !errors.Is(err2, ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld, got: %v", err2)
	}
}

func TestAcquireLock_ReleasedLockCanBeReacquired(t *testing.T) {
	t.Parallel()
	id := "lock-reacq-" + GenerateID("tx3")
	h1, err := AcquireLock(id)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	h1.Release()

	h2, err2 := AcquireLock(id)
	if err2 != nil {
		t.Fatalf("expected to reacquire after release, got: %v", err2)
	}
	defer h2.Release()
}

func TestLockHandle_ReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	id := "lock-idem-" + GenerateID("tx4")
	h, err := AcquireLock(id)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	h.Release()
	h.Release() // must not panic
}

func TestLockHandle_NilReleaseIsNoop(t *testing.T) {
	t.Parallel()
	var h *LockHandle
	h.Release() // must not panic
}

// ── Stale lock cleanup ────────────────────────────────────────────────────────

func TestCleanStaleLocks_RemovesStaleFile(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		t.Fatalf("MkdirAll: %v", mkErr)
	}

	// Write a lock record with a dead PID and an old timestamp so
	// isLockStale returns true.
	id := "stale-lock-" + GenerateID("txstale")
	path, _ := lockPath(id)

	staleRec := LockRecord{
		PID:        99999999, // almost certainly not a live PID
		SessionID:  id,
		AcquiredAt: time.Now().Add(-(StaleLockAge + time.Second)),
	}
	data, marshalErr := json.MarshalIndent(staleRec, "", "  ")
	if marshalErr != nil {
		t.Fatalf("marshal stale record: %v", marshalErr)
	}
	if writeErr := writeFileAtomic(path, data, 0o600); writeErr != nil {
		t.Fatalf("writeFileAtomic: %v", writeErr)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	removed, cleanErr := CleanStaleLocks()
	if cleanErr != nil {
		t.Fatalf("CleanStaleLocks: %v", cleanErr)
	}
	if removed == 0 {
		t.Error("expected at least one stale lock removed, got 0")
	}
}

// ── ConflictError ─────────────────────────────────────────────────────────────

func TestConflictError_IsErrSessionConflict(t *testing.T) {
	t.Parallel()
	ce := &ConflictError{SessionID: "s1", ExpectedRevision: 2, ActualRevision: 3}
	if !errors.Is(ce, ErrSessionConflict) {
		t.Error("expected errors.Is(err, ErrSessionConflict) == true")
	}
}

func TestConflictError_MessageContainsRevisions(t *testing.T) {
	t.Parallel()
	ce := &ConflictError{SessionID: "sess-abc", ExpectedRevision: 1, ActualRevision: 5}
	msg := ce.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	for _, want := range []string{"sess-abc", "1", "5"} {
		if !stringContains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}

func TestLockHeldError_IsErrLockHeld(t *testing.T) {
	t.Parallel()
	lhe := &LockHeldError{SessionID: "s1", HolderPID: 1234, Since: time.Now()}
	if !errors.Is(lhe, ErrLockHeld) {
		t.Error("expected errors.Is(err, ErrLockHeld) == true")
	}
}

// ── Concurrent save — revision check prevents silent overwrites ───────────────

func TestStore_ConcurrentSaves_RevisionConflict(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed an initial session (Revision=0 → no check on first save).
	base := testSessionData("rev-conc-1")
	base.Revision = 0
	if saveErr := store.Save(ctx, base); saveErr != nil {
		t.Fatalf("initial save: %v", saveErr)
	}

	// Load to capture revision that both writers will read.
	loaded, loadErr := store.Load(ctx, base.ID)
	if loadErr != nil {
		t.Fatalf("load: %v", loadErr)
	}
	seenRevision := loaded.Revision // should be 1

	// Writer A: saves first — revision still matches.
	writerA := *loaded
	writerA.Name = "writer-a"
	if err := store.Save(ctx, &writerA); err != nil {
		t.Fatalf("writer A save: %v", err)
	}

	// Writer B: holds the old revision — must conflict.
	writerB := *loaded
	writerB.Revision = seenRevision // deliberately stale
	writerB.Name = "writer-b"
	err = store.Save(ctx, &writerB)
	if err == nil {
		t.Fatal("expected conflict error for writer B, got nil")
	}
	if !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("expected ErrSessionConflict, got: %v", err)
	}
}

func TestStore_SaveForce_OverwritesNewerRevision(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	base := testSessionData("rev-force-1")
	base.Revision = 0
	if saveErr := store.Save(ctx, base); saveErr != nil {
		t.Fatalf("initial save: %v", saveErr)
	}

	// Advance the revision once more.
	loaded, _ := store.Load(ctx, base.ID)
	loaded.Name = "first-named-save"
	if saveErr := store.Save(ctx, loaded); saveErr != nil {
		t.Fatalf("second save: %v", saveErr)
	}

	// Force-save ignoring revision — should succeed.
	stale := *loaded
	stale.Name = "force-overwrite"
	if err := store.SaveForce(ctx, &stale); err != nil {
		t.Fatalf("SaveForce: %v", err)
	}

	final, _ := store.Load(ctx, base.ID)
	if final.Name != "force-overwrite" {
		t.Errorf("expected name %q after force, got %q", "force-overwrite", final.Name)
	}
}

func TestStore_RevisionIncrements(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	data := testSessionData("rev-inc-1")
	data.Revision = 0

	for i := int64(1); i <= 5; i++ {
		if saveErr := store.Save(ctx, data); saveErr != nil {
			t.Fatalf("save iteration %d: %v", i, saveErr)
		}
		if data.Revision != i {
			t.Errorf("iteration %d: expected revision %d, got %d", i, i, data.Revision)
		}
	}
}

// TestStore_ParallelSaves_OnlyOneWins verifies that when N goroutines race to
// save the same session using the same revision, exactly one succeeds and the
// rest receive ErrSessionConflict.
func TestStore_ParallelSaves_OnlyOneWins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	base := testSessionData("par-race-1")
	base.Revision = 0
	if saveErr := store.Save(ctx, base); saveErr != nil {
		t.Fatalf("seed: %v", saveErr)
	}
	loaded, _ := store.Load(ctx, base.ID)
	seenRevision := loaded.Revision

	const writers = 8
	errs := make([]error, writers)
	var wg sync.WaitGroup
	barrier := make(chan struct{})

	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := *loaded
			copy.Revision = seenRevision
			copy.Name = "writer"
			<-barrier // wait for all goroutines to be ready
			errs[i] = store.Save(ctx, &copy)
		}()
	}

	close(barrier) // release all goroutines simultaneously
	wg.Wait()

	successes, conflicts := 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			successes++
		case errors.Is(e, ErrSessionConflict):
			conflicts++
		default:
			t.Errorf("unexpected error type: %v", e)
		}
	}

	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
	if conflicts != writers-1 {
		t.Errorf("expected %d conflicts, got %d", writers-1, conflicts)
	}
}

// TestStore_ZeroRevision_SkipsCheck ensures that a first-time save (Revision=0)
// is never rejected, matching the "no check requested" contract.
func TestStore_ZeroRevision_SkipsCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	data := testSessionData("zero-rev-1")
	data.Revision = 0

	// First save: must always succeed regardless of what is on disk.
	if saveErr := store.Save(ctx, data); saveErr != nil {
		t.Fatalf("first zero-revision save: %v", saveErr)
	}
	// Second save with Revision=0 again: also must succeed (force semantics).
	data.Revision = 0
	if saveErr := store.Save(ctx, data); saveErr != nil {
		t.Fatalf("second zero-revision save: %v", saveErr)
	}
}

// ── helper ────────────────────────────────────────────────────────────────────

// stringContains is a dependency-free substring check used in tests.
func stringContains(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// testSessionData builds a minimal valid Data record for use in lock/revision
// tests. The suffix is appended to the ID to allow parallel tests to use
// independent sessions.
func testSessionData(suffix string) *Data {
	return &Data{
		ID:          "test-session-" + suffix,
		TxHash:      "aaaa" + suffix,
		Network:     "testnet",
		Status:      "active",
		HorizonURL:  "https://horizon-testnet.stellar.org",
		CreatedAt:   time.Now(),
		LastAccessAt: time.Now(),
		SchemaVersion: SchemaVersion,
	}
}
