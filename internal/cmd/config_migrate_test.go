// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runMigrateCmd is a test helper that executes configMigrateCmd with the
// provided args, captures stdout, and returns the output and error.
func runMigrateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Reset flags between runs to avoid state bleed.
	configMigratePathFlag = ""
	configMigrateDryRunFlag = false
	configMigrateBackupFlag = true
	configMigrateForceFlag = false

	var buf bytes.Buffer
	configMigrateCmd.SetOut(&buf)
	configMigrateCmd.SetErr(&buf)
	configMigrateCmd.SetArgs(args)

	err := configMigrateCmd.Execute()
	return buf.String(), err
}

// writeTempConfig creates a TOML file under t.TempDir() and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("writeTempConfig: %v", err)
	}
	return p
}

// ── dry-run ───────────────────────────────────────────────────────────────────

func TestConfigMigrateCmd_DryRun_WritesNothing(t *testing.T) {
	path := writeTempConfig(t, "rpc_url = \"https://example.com\"\n")
	origStat, _ := os.Stat(path)

	out, err := runMigrateCmd(t, "--path", path, "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] marker in output, got:\n%s", out)
	}

	// File must be unchanged.
	newStat, _ := os.Stat(path)
	if origStat.ModTime() != newStat.ModTime() {
		t.Error("file was modified during --dry-run")
	}
}

func TestConfigMigrateCmd_DryRun_ShowsMigratedContent(t *testing.T) {
	path := writeTempConfig(t, "rpc_url = \"https://example.com\"\n")
	out, err := runMigrateCmd(t, "--path", path, "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "schema_version = 1") {
		t.Errorf("dry-run output should show migrated content with schema_version, got:\n%s", out)
	}
}

// ── backup creation ───────────────────────────────────────────────────────────

func TestConfigMigrateCmd_BackupCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "rpc_url = \"https://example.com\"\nnetwork = \"testnet\"\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := runMigrateCmd(t, "--path", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A .bak file should exist alongside the config.
	entries, _ := os.ReadDir(dir)
	var backupFound bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			backupFound = true
			// Backup must contain original unmodified content.
			bakPath := filepath.Join(dir, e.Name())
			data, _ := os.ReadFile(bakPath)
			if string(data) != original {
				t.Errorf("backup content mismatch: got %q want %q", string(data), original)
			}
			break
		}
	}
	if !backupFound {
		t.Error("expected .bak backup file to be created")
	}
}

func TestConfigMigrateCmd_NoBackup_SkipsBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("rpc_url = \"https://example.com\"\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := runMigrateCmd(t, "--path", path, "--no-backup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			t.Errorf("no backup should have been created, but found: %s", e.Name())
		}
	}
}

// ── migration result ─────────────────────────────────────────────────────────

func TestConfigMigrateCmd_MigratesFileContent(t *testing.T) {
	path := writeTempConfig(t, "rpc_url = \"https://example.com\"\nnetwork = \"testnet\"\n")

	_, err := runMigrateCmd(t, "--path", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read migrated file: %v", readErr)
	}
	if !strings.Contains(string(data), "schema_version = 1") {
		t.Errorf("migrated file should contain schema_version = 1, got:\n%s", string(data))
	}
	// Original values must be preserved.
	if !strings.Contains(string(data), "rpc_url") {
		t.Errorf("rpc_url lost after migration:\n%s", string(data))
	}
}

func TestConfigMigrateCmd_AlreadyCurrent_NoChange(t *testing.T) {
	content := "schema_version = 1\nrpc_url = \"https://example.com\"\n"
	path := writeTempConfig(t, content)

	out, err := runMigrateCmd(t, "--path", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Nothing to migrate") {
		t.Errorf("expected 'Nothing to migrate' message, got:\n%s", out)
	}

	// Content must be unchanged.
	data, _ := os.ReadFile(path)
	if string(data) != content {
		t.Errorf("file modified despite nothing to migrate:\ngot: %s\nwant: %s", string(data), content)
	}
}

// ── future version rejection ─────────────────────────────────────────────────

func TestConfigMigrateCmd_FutureVersion_ReturnsError(t *testing.T) {
	path := writeTempConfig(t, "schema_version = 999\nrpc_url = \"https://example.com\"\n")

	_, err := runMigrateCmd(t, "--path", path)
	if err == nil {
		t.Fatal("expected error for future schema version, got nil")
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("error should mention future version 999, got: %v", err)
	}
}

func TestConfigMigrateCmd_FutureVersion_FileUnchanged(t *testing.T) {
	content := "schema_version = 999\nrpc_url = \"https://example.com\"\n"
	path := writeTempConfig(t, content)

	_, _ = runMigrateCmd(t, "--path", path)

	data, _ := os.ReadFile(path)
	if string(data) != content {
		t.Error("file must not be modified when migration is rejected for future version")
	}
}

// ── missing path ──────────────────────────────────────────────────────────────

func TestConfigMigrateCmd_NonexistentPath_ReturnsError(t *testing.T) {
	_, err := runMigrateCmd(t, "--path", "/nonexistent/path/config.toml")
	if err == nil {
		t.Fatal("expected error for nonexistent path, got nil")
	}
}

// ── idempotency ───────────────────────────────────────────────────────────────

func TestConfigMigrateCmd_Idempotent(t *testing.T) {
	path := writeTempConfig(t, "rpc_url = \"https://example.com\"\nnetwork = \"testnet\"\n")

	// First migration.
	_, err := runMigrateCmd(t, "--path", path, "--no-backup")
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	after1, _ := os.ReadFile(path)

	// Second migration — must be a no-op.
	out2, err2 := runMigrateCmd(t, "--path", path, "--no-backup")
	if err2 != nil {
		t.Fatalf("second migrate: %v", err2)
	}
	after2, _ := os.ReadFile(path)

	if string(after1) != string(after2) {
		t.Errorf("second migration changed the file:\nbefore:\n%s\nafter:\n%s", after1, after2)
	}
	if !strings.Contains(out2, "Nothing to migrate") {
		t.Errorf("second run should report nothing to migrate, got:\n%s", out2)
	}
}

// ── output format ─────────────────────────────────────────────────────────────

func TestConfigMigrateCmd_OutputShowsVersionTransition(t *testing.T) {
	path := writeTempConfig(t, "rpc_url = \"https://example.com\"\n")

	out, err := runMigrateCmd(t, "--path", path, "--no-backup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "From version") {
		t.Errorf("output should show from-version, got:\n%s", out)
	}
	if !strings.Contains(out, "To version") {
		t.Errorf("output should show to-version, got:\n%s", out)
	}
}
