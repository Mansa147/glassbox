// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"strings"
)

// MigrationResult describes what MigrateConfig did.
type MigrationResult struct {
	// FromVersion is the schema version detected in the source content.
	// 0 means the file predates versioning and is treated as version 1.
	FromVersion int
	// ToVersion is the schema version after migration (always CurrentSchemaVersion
	// when migration succeeded).
	ToVersion int
	// Changed is true when the content was actually rewritten.
	Changed bool
	// Diagnostics contains human-readable messages about each step taken.
	Diagnostics []string
}

// MigrateConfig takes raw TOML config content and returns the migrated
// content along with a MigrationResult describing what changed.
//
// Design principles:
//   - Pure function: no I/O, no side effects, safe to call from tests.
//   - Idempotent: calling twice on already-migrated content yields Changed=false.
//   - Each version boundary is a discrete, named migration step.
//   - Unknown future versions are rejected before any mutation.
//   - Comment lines (# …) are preserved unchanged.
func MigrateConfig(content string) (string, MigrationResult, error) {
	from, err := DetectSchemaVersion(content)
	if err != nil {
		return content, MigrationResult{}, fmt.Errorf("detecting schema version: %w", err)
	}

	result := MigrationResult{
		FromVersion: from,
		ToVersion:   from,
	}

	if from > CurrentSchemaVersion {
		return content, result, fmt.Errorf(
			"config file declares schema_version %d but this binary only supports up to %d; "+
				"upgrade Glassbox to migrate this file",
			from, CurrentSchemaVersion,
		)
	}

	if from == CurrentSchemaVersion {
		// Already at current version — nothing to do.
		result.Diagnostics = append(result.Diagnostics,
			fmt.Sprintf("already at schema_version %d, nothing to migrate", from))
		return content, result, nil
	}

	// Apply each migration step in sequence.
	// Each step function receives the current content and returns the (possibly
	// modified) content, a flag indicating whether it made a change, and an
	// optional diagnostic message.
	type stepFn func(string) (string, bool, string)

	// versionSteps maps the FROM version that a step upgrades away from.
	// To add a future migration: append an entry for the relevant from-version.
	steps := []struct {
		fromVersion int
		name        string
		fn          stepFn
	}{
		{
			fromVersion: 0,
			name:        "v0→v1: stamp schema_version = 1",
			fn:          migrateV0ToV1,
		},
		// Future example:
		// {
		//     fromVersion: 1,
		//     name:        "v1→v2: rename rpc_url to soroban_rpc_url",
		//     fn:          migrateV1ToV2,
		// },
	}

	out := content
	for _, step := range steps {
		if result.ToVersion != step.fromVersion {
			continue
		}
		migrated, changed, diag := step.fn(out)
		out = migrated
		if changed {
			result.Changed = true
		}
		if diag != "" {
			result.Diagnostics = append(result.Diagnostics, diag)
		}
		result.ToVersion++
	}

	return out, result, nil
}

// ── Individual migration steps ────────────────────────────────────────────────

// migrateV0ToV1 adds the schema_version = 1 line to files that predate
// versioning. The line is inserted at the top, after the leading comment
// block (if any), so that comments are preserved in place.
func migrateV0ToV1(content string) (string, bool, string) {
	lines := strings.Split(content, "\n")

	// Check idempotency: key may already be present (e.g. explicit 0 value).
	for _, line := range lines {
		k, _, ok := splitKeyVal(strings.TrimSpace(line))
		if ok && k == "schema_version" {
			return content, false, "schema_version already present, skipped insert"
		}
	}

	// Find the insertion point: after any leading blank lines or comment lines.
	insertAt := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			insertAt = i + 1
		} else {
			break
		}
	}

	// Build the new content with schema_version injected.
	newLines := make([]string, 0, len(lines)+2)
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, "schema_version = 1")
	newLines = append(newLines, "")
	newLines = append(newLines, lines[insertAt:]...)

	return strings.Join(newLines, "\n"), true, "added schema_version = 1"
}
