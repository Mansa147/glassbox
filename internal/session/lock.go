// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package session provides the lock and optimistic-revision machinery used to
// prevent concurrent Glassbox processes from silently overwriting each other's
// session saves.
//
// # Concurrency policy
//
// Two processes that want to write the same session are subject to an
// optimistic revision check: every saved session carries a monotonically
// increasing Revision counter.  When a writer reads Revision N and then
// attempts to save, it supplies the revision it read.  If the on-disk revision
// is still N, the save succeeds and the counter becomes N+1.  If another
// process saved in the meantime (disk revision is N+k, k > 0) the write is
// rejected with ErrSessionConflict, which carries the current disk revision so
// callers can decide between a force overwrite (Store.SaveForce) or an abort.
//
// In addition to the optimistic check, each active write holds a lightweight
// advisory lock file in ~/.Glassbox/locks/<session-id>.lock.  The lock is not
// mandatory (advisory), so read-only operations like List and Load never
// acquire it and are not blocked.  The lock is identified by the writing
// process's PID so stale locks left by crashed processes can be detected and
// cleaned up by the next writer.
//
// # Network filesystems
//
// On network filesystems (NFS, SMB, CIFS) advisory file locks are typically
// not enforced by the kernel.  Glassbox falls back to optimistic revision
// checks in this case; the revision check is still effective as long as both
// writers use the same SQLite database file, because SQLite's WAL mode
// serialises concurrent writes at the database level.  See
// docs/session-locking.md for details and known limitations.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrSessionConflict is the sentinel returned when an optimistic revision
// check fails.  Use errors.Is to detect it; the wrapping ConflictError carries
// the session ID and both revision numbers for diagnostics.
var ErrSessionConflict = errors.New("session write conflict")

// ConflictError is returned by Save and SaveWithValidation when the on-disk
// revision is ahead of the revision the caller last read.  It implements
// error so it can be returned directly and identified via errors.Is with the
// ErrSessionConflict sentinel.
type ConflictError struct {
	// SessionID identifies the session that was being written.
	SessionID string
	// ExpectedRevision is the revision the caller supplied (last-read value).
	ExpectedRevision int64
	// ActualRevision is the on-disk revision at the time of the check.
	ActualRevision int64
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf(
		"session %q write conflict: expected revision %d but disk has revision %d — "+
			"another process saved this session while you were editing it",
		e.SessionID, e.ExpectedRevision, e.ActualRevision,
	)
}

func (e *ConflictError) Is(target error) bool {
	return target == ErrSessionConflict
}

// Unwrap exposes the sentinel so errors.Is(err, ErrSessionConflict) works.
func (e *ConflictError) Unwrap() error {
	return ErrSessionConflict
}

// ErrLockHeld is the sentinel returned when AcquireLock finds a live lock.
var ErrLockHeld = errors.New("session advisory lock is held by another process")

// LockHeldError carries detail about who holds the lock.
type LockHeldError struct {
	SessionID string
	HolderPID int
	Since     time.Time
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf(
		"session %q is locked by process %d (since %s) — "+
			"another Glassbox instance is currently saving this session",
		e.SessionID, e.HolderPID, e.Since.Format(time.RFC3339),
	)
}

func (e *LockHeldError) Is(target error) bool {
	return target == ErrLockHeld
}

// Unwrap exposes the sentinel so errors.Is(err, ErrLockHeld) works.
func (e *LockHeldError) Unwrap() error {
	return ErrLockHeld
}

// LockRecord is the JSON payload written into an advisory lock file.
type LockRecord struct {
	// PID is the OS process that holds the lock.
	PID int `json:"pid"`
	// SessionID is the session being written.
	SessionID string `json:"session_id"`
	// AcquiredAt is the wall-clock time the lock was taken.
	AcquiredAt time.Time `json:"acquired_at"`
}

// StaleLockAge is the minimum age a lock file must have before it is
// considered stale (the owning process has most likely crashed).  Any lock
// file older than this and whose PID is no longer alive is removed
// automatically by the next writer.
const StaleLockAge = 5 * time.Minute

// lockDir returns the path of the directory that holds advisory lock files.
func lockDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory unavailable: %w", err)
	}
	return filepath.Join(home, ".Glassbox", "locks"), nil
}

// lockPath returns the filesystem path for the advisory lock file of sessionID.
func lockPath(sessionID string) (string, error) {
	dir, err := lockDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionID+".lock"), nil
}

// readLockRecord reads and unmarshals the lock record at path.
func readLockRecord(path string) (*LockRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec LockRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("malformed lock file: %w", err)
	}
	return &rec, nil
}

// isLockStale reports whether the lock record belongs to a dead process and
// the file is old enough that it is safe to steal.
func isLockStale(rec *LockRecord) bool {
	if rec.PID <= 0 {
		return true
	}
	if processAlive(rec.PID) {
		return false
	}
	// Process is gone; also require the file to be older than StaleLockAge so
	// we do not immediately steal a lock written by a process that is still
	// starting up or has just exited on the same clock tick.
	return time.Since(rec.AcquiredAt) >= StaleLockAge
}

// AcquireLock attempts to acquire the advisory lock for sessionID.
// It returns a non-nil *LockHandle on success.  If the lock is already held
// by a live process, it returns ErrLockHeld.  Stale locks from dead processes
// are removed automatically.
//
// Callers must call LockHandle.Release when the write is complete.
func AcquireLock(sessionID string) (*LockHandle, error) {
	path, err := lockPath(sessionID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory %q: %w", dir, err)
	}

	// Check for an existing lock and decide whether it is stale.
	if existing, readErr := readLockRecord(path); readErr == nil {
		if isLockStale(existing) {
			// Safe to remove: the owning process is gone and the file is old.
			_ = os.Remove(path)
		} else {
			return nil, &LockHeldError{
				SessionID: sessionID,
				HolderPID: existing.PID,
				Since:     existing.AcquiredAt,
			}
		}
	}

	record := LockRecord{
		PID:        os.Getpid(),
		SessionID:  sessionID,
		AcquiredAt: time.Now(),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lock record: %w", err)
	}

	// writeFileAtomic guarantees that the lock file is complete or absent.
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write advisory lock: %w", err)
	}

	return &LockHandle{sessionID: sessionID, path: path}, nil
}

// LockHandle represents a held advisory lock.  Release must be called when
// the guarded write is complete, whether or not it succeeded.
type LockHandle struct {
	sessionID string
	path      string
}

// Release removes the advisory lock file.  Calling Release on an already-
// released handle or when the lock file has been cleaned up is a no-op.
func (h *LockHandle) Release() {
	if h == nil || h.path == "" {
		return
	}
	_ = os.Remove(h.path)
}

// CleanStaleLocks removes advisory lock files in the lock directory whose
// owning process is dead and whose file age exceeds StaleLockAge.
// Individual removal errors are silently ignored (best effort).
// Returns the number of files removed.
func CleanStaleLocks() (int, error) {
	dir, err := lockDir()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read lock directory: %w", err)
	}

	removed := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".lock" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		rec, readErr := readLockRecord(p)
		if readErr != nil {
			// Unreadable lock file — remove it if old enough.
			info, infoErr := e.Info()
			if infoErr == nil && time.Since(info.ModTime()) >= StaleLockAge {
				if os.Remove(p) == nil {
					removed++
				}
			}
			continue
		}
		if isLockStale(rec) {
			if os.Remove(p) == nil {
				removed++
			}
		}
	}
	return removed, nil
}
