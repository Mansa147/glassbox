# Session Schema Migration Guide

This guide covers migration between major session schema versions in Glassbox.
Session data is stored in an embedded SQLite database managed by `internal/session`.

## Version History

| Version | Introduced | Changes |
|---------|-----------|---------|
| v0 | Initial release | Base session schema (no env_fingerprint) |
| v1 | Environment binding | Added `EnvFingerprint` and `PinnedEndpoint` fields |
| v2 (current) | Current | Provenance tracking, encryption, integrity checks |

## Version Detection

Glassbox automatically detects the schema version of a stored session on load.
The detection logic lives in `internal/session/schema.go` and classifies sessions
into one of four categories:

| Category | Meaning | Action |
|----------|---------|--------|
| **Current** | `StoredVersion == SchemaVersion` | No action needed |
| **NeedsUpgrade** | `MinSupportedSchemaVersion <= StoredVersion < SchemaVersion` | Automatic upgrade on load |
| **Unsupported (too old)** | `StoredVersion < MinSupportedSchemaVersion` | Re-run `glassbox debug` to recreate |
| **Unsupported (future)** | `StoredVersion > SchemaVersion` | Upgrade Glassbox binary |

You can detect the schema version programmatically:

```go
import "github.com/dotandev/glassbox/internal/session"

result := session.SchemaVersionSummary(storedVersion)
// "session schema version 1 is outdated (current: 2); Glassbox will upgrade the session automatically on load"
```

Or via CLI:

```bash
glassbox session:info --session <session-id>
```

## v0 → v1: Environment Binding

**Breaking:** No — the upgrade is automatic and adds an `EnvFingerprint` field.

**What changed:**
- Sessions gained an `EnvFingerprint` field that captures the host environment
  at debug time (OS, Go version, Stellar SDK version, network passphrase).
- Sessions gained a `PinnedEndpoint` field to record the RPC endpoint used.

**Migration behavior:**
When Glassbox loads a v0 session, `UpgradeSessionData()` in
`internal/session/schema.go:129` automatically:
1. Generates an `EnvFingerprint` from the current environment
2. Sets `SchemaVersion` to the current value (2)
3. Records a provenance entry marking the migration

**Rollback:** None needed — v0 sessions remain readable by older Glassbox
versions until the schema version field is updated (which only happens on write).

**Compatibility timeline:**
- v0 sessions can be loaded by any Glassbox version that supports
  `MinSupportedSchemaVersion <= 0`. Currently all versions support v0.
- Future versions may raise `MinSupportedSchemaVersion` to 1, at which point
  v0 sessions must be re-created via `glassbox debug <tx-hash>`.

## v1 → v2: Provenance and Encryption

**Breaking:** No — the upgrade is automatic.

**What changed:**
- Sessions gained encryption-at-rest support (AES-256-GCM via `session_key`).
- Provenance timeline tracking was added to record every mutation.
- Integrity checks (HMAC) were added to detect tampering.

**Migration behavior:**
When Glassbox loads a v1 session, `UpgradeSessionData()`:
1. Bumps `SchemaVersion` to 2
2. Records a provenance entry
3. Does not retroactively encrypt (encryption applies to new writes)

**Rollback:** v2 sessions can be read by v1 Glassbox binaries if they were
not encrypted. Encrypted sessions require a v2+ binary.

**Compatibility timeline:**
- v1 sessions without encryption remain readable by older binaries.
- Encrypted v1-upgraded sessions require Glassbox v2+.

## Manual Rollback

If you need to revert a session to a previous schema version:

```bash
# Export session data before migration
glassbox session:export --session <id> --output backup.json

# Re-create the session from scratch (generates current schema)
glassbox debug <tx-hash> --network <network>

# Or restore from backup (if you have a v1 backup)
glassbox session:import --input backup.json
```

## Troubleshooting

### "session schema version X is too old to load"

The session predates `MinSupportedSchemaVersion`. Re-create it:

```bash
glassbox debug <tx-hash> --network <network>
```

### "session schema version X was produced by a newer version"

Upgrade Glassbox:

```bash
# Via go install
go install github.com/dotandev/glassbox/cmd/glassbox@latest

# Via release binary
# Download from https://github.com/pugsley76/glassbox/releases
```

### "Glassbox will upgrade the session automatically on load"

This is informational. The session will be upgraded transparently on first use.
No action required.
