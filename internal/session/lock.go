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
// # Lock metadata
//
// Alongside the advisory lock file, AcquireLock writes a companion metadata
// file at <lock-path>.meta containing the holder's PID, hostname, acquisition
// timestamp, Glassbox version, and session ID.  When a lock acquisition fails
// because another process holds the lock, the metadata is read and included
// verbatim in the error hint so the developer knows exactly who holds the lock
// and when it was acquired.
//
// # Stale lock recovery
//
// RecoverStaleLock reads the metadata file, checks whether the recorded PID is
// still running via a zero-signal probe (processAlive), and removes both the
// lock and metadata files when the process is gone — regardless of the file
// age.  AcquireLock calls RecoverStaleLock automatically before returning a
// lock-held error, so a single retry after a successful recovery is attempted
// transparently without callers having to handle the recovery themselves.
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

	"github.com/dotandev/glassbox/internal/logger"
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
	// Metadata holds the full lock metadata read from the .meta file.
	// It is nil when the metadata file could not be read.
	Metadata *LockMetadata
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

// LockRecord is the JSON payload written into the advisory lock file (.lock).
// It contains the minimal fields needed to identify the holder and check
// liveness; the richer LockMetadata is written to the companion .meta file.
type LockRecord struct {
	// PID is the OS process that holds the lock.
	PID int `json:"pid"`
	// SessionID is the session being written.
	SessionID string `json:"session_id"`
	// AcquiredAt is the wall-clock time the lock was taken.
	AcquiredAt time.Time `json:"acquired_at"`
}

// LockMetadata is the JSON payload written into the companion metadata file
// (<lock-path>.meta).  It carries the full set of fields needed to produce
// an actionable diagnostic when a lock acquisition fails.
type LockMetadata struct {
	// PID is the OS process ID of the lock holder.
	PID int `json:"pid"`
	// Hostname is the machine name of the lock holder.
	Hostname string `json:"hostname"`
	// AcquiredAt is the wall-clock time the lock was acquired.
	AcquiredAt time.Time `json:"acquired_at"`
	// GlassboxVersion is the version string of the holding Glassbox binary.
	GlassboxVersion string `json:"glassbox_version"`
	// SessionID is the session being written.
	SessionID string `json:"session_id"`
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

// metaPath returns the filesystem path for the lock metadata file of sessionID.
// The metadata file lives alongside the .lock file with the extension .lock.meta.
func metaPath(lockFilePath string) string {
	return lockFilePath + ".meta"
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

// ReadLockMetadata reads and parses the lock metadata file at the companion
// .meta path alongside lockFilePath.  Returns a nil pointer (not an error)
// when the file does not exist.  Returns an error only when the file exists
// but is unreadable or malformed.
func ReadLockMetadata(lockFilePath string) (*LockMetadata, error) {
	mp := metaPath(lockFilePath)
	data, err := os.ReadFile(mp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read lock metadata at %q: %w", mp, err)
	}
	var meta LockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("malformed lock metadata at %q: %w", mp, err)
	}
	return &meta, nil
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

// RecoverStaleLock attempts to remove a stale advisory lock for the given
// session.  It reads the lock metadata file to check whether the recorded PID
// is still running:
//
//   - If the PID is NOT running, both the .lock and .meta files are removed
//     and (true, nil) is returned.
//   - If the PID IS running, no files are touched and (false, nil) is returned.
//   - If the PID lookup fails for any reason, no files are touched and
//     (false, err) is returned.
//
// RecoverStaleLock does NOT respect StaleLockAge: it relies solely on the
// liveness of the PID.  This is intentional — it is used for explicit
// operator-triggered recovery (glassbox session recover) and for the automatic
// retry in AcquireLock, both of which should not wait 5 minutes.
func RecoverStaleLock(sessionPath string) (bool, error) {
	// sessionPath is the full path to the .lock file.
	lp := sessionPath

	rec, readErr := readLockRecord(lp)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			// Lock does not exist; nothing to recover.
			return false, nil
		}
		return false, fmt.Errorf("RecoverStaleLock: cannot read lock file %q: %w", lp, readErr)
	}

	if rec.PID <= 0 {
		// Malformed PID — treat as stale and remove.
		_ = os.Remove(metaPath(lp))
		if err := os.Remove(lp); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("RecoverStaleLock: failed to remove lock file %q: %w", lp, err)
		}
		return true, nil
	}

	alive := processAlive(rec.PID)
	if alive {
		return false, nil
	}

	// Process is gone — remove both files.
	_ = os.Remove(metaPath(lp))
	if err := os.Remove(lp); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("RecoverStaleLock: failed to remove lock file %q: %w", lp, err)
	}
	return true, nil
}

// acquireOnce is the inner acquisition attempt used by AcquireLock.  It tries
// to claim the lock file, reads any existing record, and returns either the
// LockHandle on success or the blocker's metadata on conflict.
func acquireOnce(sessionID, path, glassboxVersion string) (*LockHandle, *LockHeldError, error) {
	// Check for an existing lock and decide whether it is stale.
	if existing, readErr := readLockRecord(path); readErr == nil {
		// Read the companion metadata (best-effort; nil is fine if absent).
		meta, _ := ReadLockMetadata(path)

		if isLockStale(existing) {
			// Safe to remove: the owning process is gone and the file is old.
			_ = os.Remove(metaPath(path))
			_ = os.Remove(path)
		} else {
			return nil, &LockHeldError{
				SessionID: sessionID,
				HolderPID: existing.PID,
				Since:     existing.AcquiredAt,
				Metadata:  meta,
			}, nil
		}
	}

	// Write the lock record.
	record := LockRecord{
		PID:        os.Getpid(),
		SessionID:  sessionID,
		AcquiredAt: time.Now(),
	}
	lockData, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal lock record: %w", err)
	}
	// writeFileAtomic guarantees that the lock file is complete or absent.
	if err := writeFileAtomic(path, lockData, 0o600); err != nil {
		return nil, nil, fmt.Errorf("failed to write advisory lock: %w", err)
	}

	// Write the companion metadata file atomically.
	hostname, _ := os.Hostname()
	meta := LockMetadata{
		PID:             os.Getpid(),
		Hostname:        hostname,
		AcquiredAt:      record.AcquiredAt,
		GlassboxVersion: glassboxVersion,
		SessionID:       sessionID,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		// Metadata write failure is non-fatal: the advisory lock is already in
		// place.  Log the problem and continue without the metadata file.
		logger.Logger.Warn("failed to marshal lock metadata — lock acquired without metadata",
			"session_id", sessionID, "error", err)
	} else if writeErr := writeFileAtomic(metaPath(path), metaData, 0o600); writeErr != nil {
		logger.Logger.Warn("failed to write lock metadata file — lock acquired without metadata",
			"session_id", sessionID, "path", metaPath(path), "error", writeErr)
	}

	return &LockHandle{sessionID: sessionID, path: path}, nil, nil
}

// AcquireLock attempts to acquire the advisory lock for sessionID.
// It returns a non-nil *LockHandle on success.
//
// On first attempt, if the lock is held by a live process the function calls
// RecoverStaleLock once.  If the stale recovery succeeds (the holder's PID
// was dead), it logs an INFO message and retries the acquisition exactly once.
// If the retry also finds the lock held, or if the process is genuinely alive,
// it returns a *LockHeldError that includes the holder's full metadata
// (PID, hostname, acquisition time, Glassbox version) in its Metadata field so
// callers can build an actionable error message.
//
// Callers must call LockHandle.Release when the write is complete.
func AcquireLock(sessionID string) (*LockHandle, error) {
	return acquireLockWithVersion(sessionID, glassboxVersionForLock())
}

// acquireLockWithVersion is the testable inner form of AcquireLock; it accepts
// the Glassbox version string explicitly so unit tests can inject a known value.
func acquireLockWithVersion(sessionID, glassboxVersion string) (*LockHandle, error) {
	path, err := lockPath(sessionID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory %q: %w", dir, err)
	}

	handle, heldErr, acquireErr := acquireOnce(sessionID, path, glassboxVersion)
	if acquireErr != nil {
		return nil, acquireErr
	}
	if handle != nil {
		return handle, nil
	}

	// Lock is held.  Attempt automatic stale-lock recovery before surfacing
	// the error to the caller.
	recovered, recoverErr := RecoverStaleLock(path)
	if recoverErr != nil {
		// Recovery attempt failed — surface original conflict with metadata.
		return nil, lockHeldErstError(heldErr)
	}

	if recovered {
		// Stale lock removed; log and retry once.
		logger.Logger.Info("stale advisory lock recovered automatically",
			"session_id", sessionID,
			"previous_pid", heldErr.HolderPID,
			"acquired_at", heldErr.Since.Format(time.RFC3339),
		)
		handle2, heldErr2, acquireErr2 := acquireOnce(sessionID, path, glassboxVersion)
		if acquireErr2 != nil {
			return nil, acquireErr2
		}
		if handle2 != nil {
			return handle2, nil
		}
		// Still held after recovery — surface conflict.
		return nil, lockHeldErstError(heldErr2)
	}

	// Process is still alive — genuine conflict.
	return nil, lockHeldErstError(heldErr)
}

// glassboxVersionForLock returns the Glassbox version string to embed in lock
// metadata.  It is a separate function so the import of internal/version stays
// here and doesn't cause an import cycle if lock.go is imported by version's
// own tests.
func glassboxVersionForLock() string {
	// Imported lazily via a package-level variable set at startup to avoid
	// circular dependencies.  Falls back to "unknown" when not set.
	if glassboxLockVersion != "" {
		return glassboxLockVersion
	}
	return "unknown"
}

// glassboxLockVersion is set by session package init via SetGlassboxVersion to
// avoid an import cycle between internal/session and internal/version.
var glassboxLockVersion string

// SetGlassboxVersion injects the Glassbox version into the session lock system
// so it is embedded in lock metadata files.  Call this once at program startup
// (e.g. in cmd/glassbox/main.go) after the version package is initialised.
func SetGlassboxVersion(v string) {
	glassboxLockVersion = v
}

// lockHeldErstError converts a *LockHeldError into an ErstError (via the
// internal/errors package) enriched with the holder's full metadata hint.
// Because lock.go cannot import internal/errors without a cycle, the hint is
// embedded directly into the LockHeldError and the caller (cmd/session.go or
// store.go) is responsible for wrapping it into an ErstError.  To make the
// metadata available to those callers, we return the *LockHeldError unchanged
// and let the upper layers format the hint.
func lockHeldErstError(e *LockHeldError) error {
	if e == nil {
		return ErrLockHeld
	}
	return e
}

// LockHandle represents a held advisory lock.  Release must be called when
// the guarded write is complete, whether or not it succeeded.
type LockHandle struct {
	sessionID string
	path      string
}

// Release removes the advisory lock file and its companion metadata file.
// Calling Release on an already-released handle or when the lock file has been
// cleaned up is a no-op.
func (h *LockHandle) Release() {
	if h == nil || h.path == "" {
		return
	}
	_ = os.Remove(metaPath(h.path))
	_ = os.Remove(h.path)
}

// CleanStaleLocks removes advisory lock files in the lock directory whose
// owning process is dead and whose file age exceeds StaleLockAge.
// Companion .meta files are removed alongside their .lock files.
// Individual removal errors are silently ignored (best effort).
// Returns the number of .lock files removed.
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
				_ = os.Remove(metaPath(p))
				if os.Remove(p) == nil {
					removed++
				}
			}
			continue
		}
		if isLockStale(rec) {
			_ = os.Remove(metaPath(p))
			if os.Remove(p) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// ── Exported helpers for cmd/session.go ──────────────────────────────────────

// LockPathForSession returns the absolute filesystem path to the advisory lock
// file for the given sessionID.  This is exported so CLI commands can pass the
// path to ReadLockMetadata or RecoverStaleLock without duplicating the path
// construction logic.
func LockPathForSession(sessionID string) (string, error) {
	return lockPath(sessionID)
}

// ReadLockRecord is the exported form of readLockRecord, used by CLI commands
// that need to display raw lock information when the metadata file is absent.
func ReadLockRecord(path string) (*LockRecord, error) {
	return readLockRecord(path)
}
