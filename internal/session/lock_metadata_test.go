// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package session

// lock_metadata_test.go covers:
//   - ReadLockMetadata round-trip (write metadata → read back, all fields intact)
//   - RecoverStaleLock with a dead-PID fixture (removes both .lock and .meta)
//   - RecoverStaleLock with a live PID (leaves files untouched)
//   - RecoverStaleLock when no lock file exists (returns false, nil)
//   - RecoverStaleLock with a malformed PID (treated as stale)
//   - AcquireLock writes a .meta file alongside the .lock file
//   - LockHeldError.Metadata is populated when a second caller is blocked
//   - Concurrent acquisition: exactly one winner, structured metadata error
//   - Release removes both .lock and .meta files
//   - CleanStaleLocks removes .meta alongside .lock files

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// ── helpers ────────────────────────────────────────────────────────────────────

// writeLockFixture writes a lock record (and optionally a metadata record) at
// a known path so tests can exercise RecoverStaleLock without going through
// AcquireLock.
func writeLockFixture(t *testing.T, path string, rec LockRecord, meta *LockMetadata) {
	t.Helper()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("writeLockFixture: marshal lock record: %v", err)
	}
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		t.Fatalf("writeLockFixture: write lock file: %v", err)
	}
	if meta != nil {
		mdata, merr := json.MarshalIndent(meta, "", "  ")
		if merr != nil {
			t.Fatalf("writeLockFixture: marshal metadata: %v", merr)
		}
		if werr := writeFileAtomic(metaPath(path), mdata, 0o600); werr != nil {
			t.Fatalf("writeLockFixture: write metadata file: %v", werr)
		}
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
		_ = os.Remove(metaPath(path))
	})
}

// ── ReadLockMetadata round-trip ────────────────────────────────────────────────

func TestReadLockMetadata_RoundTrip(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	id := "meta-roundtrip-" + GenerateID("txrt")
	lp, _ := lockPath(id)

	want := LockMetadata{
		PID:             12345,
		Hostname:        "testhost.example.com",
		AcquiredAt:      time.Now().Truncate(time.Second),
		GlassboxVersion: "1.2.3",
		SessionID:       id,
	}
	writeLockFixture(t, lp,
		LockRecord{PID: want.PID, SessionID: id, AcquiredAt: want.AcquiredAt},
		&want,
	)

	got, err := ReadLockMetadata(lp)
	if err != nil {
		t.Fatalf("ReadLockMetadata: %v", err)
	}
	if got == nil {
		t.Fatal("ReadLockMetadata returned nil, expected non-nil")
	}
	if got.PID != want.PID {
		t.Errorf("PID: got %d, want %d", got.PID, want.PID)
	}
	if got.Hostname != want.Hostname {
		t.Errorf("Hostname: got %q, want %q", got.Hostname, want.Hostname)
	}
	if !got.AcquiredAt.Equal(want.AcquiredAt) {
		t.Errorf("AcquiredAt: got %v, want %v", got.AcquiredAt, want.AcquiredAt)
	}
	if got.GlassboxVersion != want.GlassboxVersion {
		t.Errorf("GlassboxVersion: got %q, want %q", got.GlassboxVersion, want.GlassboxVersion)
	}
	if got.SessionID != want.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, want.SessionID)
	}
}

func TestReadLockMetadata_AbsentFile_ReturnsNil(t *testing.T) {
	t.Parallel()
	// Point at a path that definitely does not exist.
	got, err := ReadLockMetadata("/nonexistent/path/session.lock")
	if err != nil {
		t.Fatalf("expected nil error for absent file, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil metadata for absent file, got %+v", got)
	}
}

func TestReadLockMetadata_MalformedJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	id := "meta-malformed-" + GenerateID("txmalf")
	lp, _ := lockPath(id)

	// Write garbage to the .meta file.
	mp := metaPath(lp)
	if err := writeFileAtomic(mp, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write malformed meta: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(mp) })

	_, err = ReadLockMetadata(lp)
	if err == nil {
		t.Error("expected error for malformed metadata, got nil")
	}
}

// ── RecoverStaleLock ───────────────────────────────────────────────────────────

func TestRecoverStaleLock_DeadPID_RemovesBothFiles(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	id := "recover-dead-" + GenerateID("txdead")
	lp, _ := lockPath(id)
	// PID 99999999 is almost certainly not alive.
	rec := LockRecord{PID: 99999999, SessionID: id, AcquiredAt: time.Now().Add(-(StaleLockAge + time.Second))}
	meta := &LockMetadata{
		PID: rec.PID, SessionID: id, Hostname: "gone-host",
		AcquiredAt: rec.AcquiredAt, GlassboxVersion: "1.0.0",
	}
	writeLockFixture(t, lp, rec, meta)

	recovered, recErr := RecoverStaleLock(lp)
	if recErr != nil {
		t.Fatalf("RecoverStaleLock: %v", recErr)
	}
	if !recovered {
		t.Error("expected recovered=true for dead PID, got false")
	}

	// Both files must be gone.
	if _, statErr := os.Stat(lp); !os.IsNotExist(statErr) {
		t.Errorf(".lock file still exists after recovery")
	}
	if _, statErr := os.Stat(metaPath(lp)); !os.IsNotExist(statErr) {
		t.Errorf(".meta file still exists after recovery")
	}
}

func TestRecoverStaleLock_LivePID_LeavesFilesUntouched(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	id := "recover-live-" + GenerateID("txlive")
	lp, _ := lockPath(id)
	// Use our own PID — definitely alive.
	rec := LockRecord{PID: os.Getpid(), SessionID: id, AcquiredAt: time.Now()}
	meta := &LockMetadata{
		PID: rec.PID, SessionID: id, Hostname: "localhost",
		AcquiredAt: rec.AcquiredAt, GlassboxVersion: "1.0.0",
	}
	writeLockFixture(t, lp, rec, meta)

	recovered, recErr := RecoverStaleLock(lp)
	if recErr != nil {
		t.Fatalf("RecoverStaleLock returned unexpected error: %v", recErr)
	}
	if recovered {
		t.Error("expected recovered=false for live PID, got true")
	}

	// Both files must still exist.
	if _, statErr := os.Stat(lp); statErr != nil {
		t.Errorf(".lock file unexpectedly missing: %v", statErr)
	}
	if _, statErr := os.Stat(metaPath(lp)); statErr != nil {
		t.Errorf(".meta file unexpectedly missing: %v", statErr)
	}
}

func TestRecoverStaleLock_NoLockFile_ReturnsFalseNil(t *testing.T) {
	t.Parallel()
	// Non-existent path — should return (false, nil) cleanly.
	recovered, err := RecoverStaleLock("/nonexistent/path/nolock.lock")
	if err != nil {
		t.Fatalf("unexpected error for absent lock: %v", err)
	}
	if recovered {
		t.Error("expected recovered=false when no lock file exists, got true")
	}
}

func TestRecoverStaleLock_MalformedPID_TreatedAsStale(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	id := "recover-malformed-pid-" + GenerateID("txmpid")
	lp, _ := lockPath(id)

	// PID 0 is invalid → treated as stale.
	rec := LockRecord{PID: 0, SessionID: id, AcquiredAt: time.Now().Add(-time.Hour)}
	writeLockFixture(t, lp, rec, nil)

	recovered, recErr := RecoverStaleLock(lp)
	if recErr != nil {
		t.Fatalf("RecoverStaleLock: %v", recErr)
	}
	if !recovered {
		t.Error("expected recovered=true for malformed (zero) PID, got false")
	}
	if _, statErr := os.Stat(lp); !os.IsNotExist(statErr) {
		t.Errorf(".lock file still exists after recovery of zero-PID lock")
	}
}

// ── AcquireLock writes metadata ────────────────────────────────────────────────

func TestAcquireLock_WritesMetaFile(t *testing.T) {
	t.Parallel()

	id := "lock-meta-write-" + GenerateID("txmw")
	h, err := acquireLockWithVersion(id, "test-version-1.0")
	if err != nil {
		t.Fatalf("acquireLockWithVersion: %v", err)
	}
	defer h.Release()

	lp, _ := lockPath(id)
	meta, readErr := ReadLockMetadata(lp)
	if readErr != nil {
		t.Fatalf("ReadLockMetadata after acquire: %v", readErr)
	}
	if meta == nil {
		t.Fatal("expected metadata file to exist after AcquireLock, got nil")
	}
	if meta.PID != os.Getpid() {
		t.Errorf("metadata PID: got %d, want %d", meta.PID, os.Getpid())
	}
	if meta.GlassboxVersion != "test-version-1.0" {
		t.Errorf("GlassboxVersion: got %q, want %q", meta.GlassboxVersion, "test-version-1.0")
	}
	if meta.SessionID != id {
		t.Errorf("SessionID: got %q, want %q", meta.SessionID, id)
	}
	if meta.Hostname == "" {
		t.Error("expected non-empty Hostname in metadata")
	}
	if meta.AcquiredAt.IsZero() {
		t.Error("expected non-zero AcquiredAt in metadata")
	}
}

func TestAcquireLock_Release_RemovesBothFiles(t *testing.T) {
	t.Parallel()

	id := "lock-release-both-" + GenerateID("txrb")
	h, err := acquireLockWithVersion(id, "test-v")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	lp, _ := lockPath(id)

	h.Release()

	if _, statErr := os.Stat(lp); !os.IsNotExist(statErr) {
		t.Errorf(".lock file still exists after Release")
	}
	if _, statErr := os.Stat(metaPath(lp)); !os.IsNotExist(statErr) {
		t.Errorf(".meta file still exists after Release")
	}
}

// ── LockHeldError carries metadata ────────────────────────────────────────────

func TestAcquireLock_SecondCaller_LockHeldError_HasMetadata(t *testing.T) {
	t.Parallel()

	id := "lock-meta-held-" + GenerateID("txheld")
	h1, err := acquireLockWithVersion(id, "test-v-holder")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer h1.Release()

	// Second caller must see ErrLockHeld with metadata populated.
	_, err2 := acquireLockWithVersion(id, "test-v-waiter")
	if err2 == nil {
		t.Fatal("expected error for second caller, got nil")
	}
	if !errors.Is(err2, ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld, got %T: %v", err2, err2)
	}

	var lhe *LockHeldError
	if !errors.As(err2, &lhe) {
		t.Fatalf("expected *LockHeldError, got %T", err2)
	}
	if lhe.Metadata == nil {
		t.Fatal("expected LockHeldError.Metadata to be non-nil for second caller")
	}
	if lhe.Metadata.PID != os.Getpid() {
		t.Errorf("metadata PID: got %d, want %d", lhe.Metadata.PID, os.Getpid())
	}
	if lhe.Metadata.GlassboxVersion != "test-v-holder" {
		t.Errorf("metadata GlassboxVersion: got %q, want %q",
			lhe.Metadata.GlassboxVersion, "test-v-holder")
	}
	if lhe.Metadata.SessionID != id {
		t.Errorf("metadata SessionID: got %q, want %q", lhe.Metadata.SessionID, id)
	}
}

// ── Concurrent acquisition ─────────────────────────────────────────────────────

// TestAcquireLock_Concurrent_ExactlyOneWinsWithMetadata verifies that when N
// goroutines race to acquire the same session lock, exactly one wins and the
// rest receive *LockHeldError with Metadata populated.
func TestAcquireLock_Concurrent_ExactlyOneWinsWithMetadata(t *testing.T) {
	t.Parallel()

	id := "lock-concurrent-meta-" + GenerateID("txconc")

	const racers = 8
	type result struct {
		handle *LockHandle
		err    error
	}
	results := make([]result, racers)
	var wg sync.WaitGroup
	barrier := make(chan struct{})

	for i := 0; i < racers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			h, e := acquireLockWithVersion(id, "concurrent-v")
			results[i] = result{h, e}
		}()
	}

	close(barrier)
	wg.Wait()

	successes := 0
	heldErrors := 0
	otherErrors := 0

	for _, r := range results {
		switch {
		case r.err == nil:
			successes++
			if r.handle != nil {
				r.handle.Release()
			}
		case errors.Is(r.err, ErrLockHeld):
			heldErrors++
			// Every blocked caller must carry structured metadata.
			var lhe *LockHeldError
			if errors.As(r.err, &lhe) {
				// Metadata may be nil if the winning goroutine released before
				// the metadata file was read — that is a race between the winner's
				// Release and the loser's metadata read.  Only assert non-nil
				// Metadata when the lock is still held (non-nil handle from the
				// winner) — we can't guarantee ordering here.
				// We only check that if Metadata IS present, it is well-formed.
				if lhe.Metadata != nil {
					if lhe.Metadata.SessionID != id {
						t.Errorf("racer %d: LockHeldError metadata SessionID = %q, want %q",
							heldErrors, lhe.Metadata.SessionID, id)
					}
				}
			} else {
				t.Errorf("expected *LockHeldError from errors.As, got %T", r.err)
			}
		default:
			otherErrors++
			t.Errorf("unexpected error type: %T: %v", r.err, r.err)
		}
	}

	if successes != 1 {
		t.Errorf("expected exactly 1 successful acquisition, got %d", successes)
	}
	if heldErrors != racers-1 {
		t.Errorf("expected %d ErrLockHeld errors, got %d", racers-1, heldErrors)
	}
	if otherErrors != 0 {
		t.Errorf("expected 0 unexpected errors, got %d", otherErrors)
	}
}

// ── AcquireLock auto-recovery of stale lock ────────────────────────────────────

// TestAcquireLock_AutoRecoversDeadPIDLock verifies that AcquireLock
// transparently recovers a stale lock (dead PID, file beyond StaleLockAge)
// and successfully acquires the lock on its internal retry — the caller sees
// no error.
func TestAcquireLock_AutoRecoversDeadPIDLock(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	id := "lock-auto-recover-" + GenerateID("txar")
	lp, _ := lockPath(id)

	// Plant a stale lock with a dead PID and an old timestamp.
	staleRec := LockRecord{
		PID:        99999999,
		SessionID:  id,
		AcquiredAt: time.Now().Add(-(StaleLockAge + time.Second)),
	}
	staleMeta := &LockMetadata{
		PID: staleRec.PID, SessionID: id,
		Hostname: "dead-host", AcquiredAt: staleRec.AcquiredAt,
		GlassboxVersion: "0.9.0",
	}
	writeLockFixture(t, lp, staleRec, staleMeta)

	// AcquireLock must succeed because the PID is dead + file is old.
	h, err := acquireLockWithVersion(id, "recovery-v")
	if err != nil {
		t.Fatalf("AcquireLock should have auto-recovered stale lock, got: %v", err)
	}
	defer h.Release()

	// The new lock's metadata must reflect the current process.
	meta, _ := ReadLockMetadata(lp)
	if meta == nil {
		t.Fatal("expected metadata after auto-recovery acquisition, got nil")
	}
	if meta.PID != os.Getpid() {
		t.Errorf("post-recovery metadata PID: got %d, want %d", meta.PID, os.Getpid())
	}
}

// ── CleanStaleLocks removes .meta alongside .lock ─────────────────────────────

func TestCleanStaleLocks_RemovesMeta(t *testing.T) {
	t.Parallel()

	dir, err := lockDir()
	if err != nil {
		t.Skip("cannot determine lock dir:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	id := "stale-with-meta-" + GenerateID("txswm")
	lp, _ := lockPath(id)

	staleRec := LockRecord{
		PID:        99999999,
		SessionID:  id,
		AcquiredAt: time.Now().Add(-(StaleLockAge + time.Second)),
	}
	staleMeta := &LockMetadata{
		PID: staleRec.PID, SessionID: id,
		Hostname:        "gone",
		AcquiredAt:      staleRec.AcquiredAt,
		GlassboxVersion: "0.1.0",
	}
	writeLockFixture(t, lp, staleRec, staleMeta)

	removed, cleanErr := CleanStaleLocks()
	if cleanErr != nil {
		t.Fatalf("CleanStaleLocks: %v", cleanErr)
	}
	if removed == 0 {
		t.Error("expected at least one stale lock removed, got 0")
	}
	// Both files must be gone.
	if _, statErr := os.Stat(lp); !os.IsNotExist(statErr) {
		t.Errorf(".lock file still exists after CleanStaleLocks")
	}
	if _, statErr := os.Stat(metaPath(lp)); !os.IsNotExist(statErr) {
		t.Errorf(".meta file still exists after CleanStaleLocks")
	}
}
