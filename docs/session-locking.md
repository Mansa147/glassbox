# Session Locking and Concurrent-Writer Handling

Closes #813.

Two Glassbox processes saving the same session simultaneously can produce lost
annotations or corrupt metadata. This document describes the two-layer
concurrency policy that prevents silent overwrites.

---

## Overview

Glassbox protects session saves with:

1. **Advisory lock file** — a per-session `.lock` file in
   `~/.Glassbox/locks/<session-id>.lock` serialises concurrent writers.
2. **Optimistic revision check** — every session row carries a monotonically
   increasing `revision` counter. A writer that reads revision *N* and then
   saves is rejected if the on-disk revision is no longer *N*, which means
   another process saved in the meantime.

Read-only operations (`session list`, `session resume`) never acquire the
advisory lock and are never blocked.

---

## Advisory lock lifecycle

| Event | Action |
|-------|--------|
| Writer begins a save | `AcquireLock(sessionID)` — writes `~/.Glassbox/locks/<id>.lock` with `{pid, session_id, acquired_at}` |
| Write completes (success or error) | `LockHandle.Release()` — removes the lock file |
| Process crashes before release | Lock file remains on disk |
| Next writer finds the lock file | Checks whether the PID in the file is still alive |
| PID is alive | Returns `ErrLockHeld` — do not steal the lock |
| PID is dead **and** file is ≥ 5 minutes old | Removes the stale lock and proceeds |

Lock files are written atomically (`writeFileAtomic`) so a crash during lock
acquisition never leaves a truncated file.

### Stale-lock cleanup

`session doctor` calls `CleanStaleLocks()` which scans the locks directory and
removes any file whose owning PID is dead and whose age exceeds
`StaleLockAge` (5 minutes). The same cleanup runs automatically the next time
a writer tries to acquire the lock for a stale session.

---

## Optimistic revision check

Every `Data` record contains a `Revision int64` field (persisted in the
`sessions` table as a `NOT NULL DEFAULT 0` column for backwards compatibility).

| Scenario | Behaviour |
|----------|-----------|
| `data.Revision == 0` | No check — "new session or force" semantics |
| `data.Revision > 0` and disk revision equals `data.Revision` | Write proceeds; revision is incremented to `data.Revision + 1` |
| `data.Revision > 0` and disk revision **differs** | `Save` returns `*ConflictError` wrapping `ErrSessionConflict` |

### ConflictError

```go
type ConflictError struct {
    SessionID        string
    ExpectedRevision int64
    ActualRevision   int64
}
```

`errors.Is(err, session.ErrSessionConflict)` returns `true`.

The CLI surfaces this as:

```
Error: SESSION_WRITE_CONFLICT: session "abc123" write conflict: expected revision 2 but disk has revision 3
Hint:  Run 'glassbox session resume abc123' to reload the latest version and re-apply
       your changes, or re-run with --force to overwrite it.
```

Exit code: **1** (`ExitUserError`) — the user can recover without reinstalling.

---

## Force save

```bash
glassbox session save --force
```

`--force` calls `Store.SaveForce` which resets `data.Revision` to 0 before
calling `Save`, bypassing the optimistic check while still holding the
advisory lock.

Use `--force` only when you have inspected the conflict and consciously want
to overwrite the newer version.

---

## Stable error codes

| Code | Meaning | Exit |
|------|---------|------|
| `SESSION_WRITE_CONFLICT` | Concurrent writer saved a newer revision | 1 |
| `SESSION_LOCK_HELD` | Advisory lock held by a live process | 1 |

Both codes are registered in `internal/errors/glassbox_error_code.go` and map
to `ExitUserError` in `internal/cmd/exitcode.go`.

---

## Network filesystems (NFS, SMB, CIFS)

Advisory file locks are **not reliably enforced** by the kernel on network
filesystems. In such environments:

- The lock-file mechanism may not prevent concurrent access.
- The **optimistic revision check** at the SQLite layer is still effective,
  because SQLite WAL mode serialises concurrent writers at the database level.
- If two processes write to the same SQLite file over NFS simultaneously,
  SQLite may return a `SQLITE_BUSY` or `SQLITE_LOCKED` error; Glassbox will
  surface this as a generic "failed to save session" message rather than
  `SESSION_WRITE_CONFLICT`.

**Recommendation:** for shared team workflows, use a single Glassbox instance
per database file, or place the database on a local filesystem and synchronise
sessions via `glassbox session share` / `glassbox session import`.

---

## Implementation reference

| Symbol | File | Description |
|--------|------|-------------|
| `AcquireLock` | `internal/session/lock.go` | Acquire advisory lock |
| `LockHandle.Release` | `internal/session/lock.go` | Release advisory lock |
| `CleanStaleLocks` | `internal/session/lock.go` | Remove dead-process locks |
| `ErrSessionConflict` | `internal/session/lock.go` | Conflict sentinel |
| `ConflictError` | `internal/session/lock.go` | Structured conflict error |
| `ErrLockHeld` | `internal/session/lock.go` | Lock-held sentinel |
| `LockHeldError` | `internal/session/lock.go` | Structured lock-held error |
| `Store.Save` | `internal/session/store.go` | Acquire lock + revision check + save |
| `Store.SaveForce` | `internal/session/store.go` | Force-overwrite (bypass revision check) |
| `Data.Revision` | `internal/session/store.go` | Monotonic revision counter |
| `ErstSessionConflict` | `internal/errors/glassbox_error_code.go` | Stable error code |
| `ErstSessionLockHeld` | `internal/errors/glassbox_error_code.go` | Stable error code |
