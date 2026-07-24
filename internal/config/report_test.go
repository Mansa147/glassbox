// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── BuildResolveReport: basic field presence ──────────────────────────────────

func TestBuildResolveReport_DefaultsAreLabelled(t *testing.T) {
	// Load a minimal config so defaults kick in via configDefaultsAssigner.
	cfg := DefaultConfig()
	report := BuildResolveReport(cfg, "")

	// log_level, network, request_timeout, max_trace_depth should all be present
	// and labelled as defaults because nothing overrode them.
	want := map[string]bool{
		"log_level":       true,
		"network":         true,
		"request_timeout": true,
		"max_trace_depth": true,
	}

	found := map[string]ResolvedValue{}
	for _, rv := range report.Fields {
		found[rv.Field] = rv
	}

	for key := range want {
		rv, ok := found[key]
		if !ok {
			t.Errorf("field %q missing from report", key)
			continue
		}
		if rv.Source != SourceDefault {
			t.Errorf("field %q: Source = %q, want %q", key, rv.Source, SourceDefault)
		}
		if !rv.DefaultApplied {
			t.Errorf("field %q: DefaultApplied = false, want true", key)
		}
	}
}

func TestBuildResolveReport_FieldsAreStablyOrdered(t *testing.T) {
	cfg := DefaultConfig()

	r1 := BuildResolveReport(cfg, "")
	r2 := BuildResolveReport(cfg, "")

	if len(r1.Fields) != len(r2.Fields) {
		t.Fatalf("field count differs between two calls: %d vs %d", len(r1.Fields), len(r2.Fields))
	}
	for i := range r1.Fields {
		if r1.Fields[i].Field != r2.Fields[i].Field {
			t.Errorf("field order differs at index %d: %q vs %q", i, r1.Fields[i].Field, r2.Fields[i].Field)
		}
	}
}

// ── BuildResolveReport: environment source ────────────────────────────────────

func TestBuildResolveReport_EnvVarSource(t *testing.T) {
	orig := os.Getenv("GLASSBOX_LOG_LEVEL")
	t.Cleanup(func() { os.Setenv("GLASSBOX_LOG_LEVEL", orig) })
	os.Setenv("GLASSBOX_LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	report := BuildResolveReport(cfg, "")

	for _, rv := range report.Fields {
		if rv.Field != "log_level" {
			continue
		}
		if rv.Source != SourceEnvironment {
			t.Errorf("log_level Source = %q, want %q", rv.Source, SourceEnvironment)
		}
		if rv.EnvVar != "GLASSBOX_LOG_LEVEL" {
			t.Errorf("log_level EnvVar = %q, want GLASSBOX_LOG_LEVEL", rv.EnvVar)
		}
		if rv.EffectiveValue != "debug" {
			t.Errorf("log_level EffectiveValue = %q, want debug", rv.EffectiveValue)
		}
		return
	}
	t.Error("log_level not found in report fields")
}

// ── BuildResolveReport: file source ──────────────────────────────────────────

func TestBuildResolveReport_FileSource(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	// Write a project config with a custom log_level.
	configPath := filepath.Join(project, ".glassbox.toml")
	if err := os.WriteFile(configPath, []byte(
		"rpc_url = \"https://file.example.com\"\nnetwork = \"testnet\"\nlog_level = \"warn\"\n",
	), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	// Clear log-level env var so the file value wins.
	origLogLevel := os.Getenv("GLASSBOX_LOG_LEVEL")
	t.Cleanup(func() { os.Setenv("GLASSBOX_LOG_LEVEL", origLogLevel) })
	os.Unsetenv("GLASSBOX_LOG_LEVEL")

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	activeFile := ActiveConfigFile()
	report := BuildResolveReport(cfg, activeFile)

	for _, rv := range report.Fields {
		if rv.Field != "log_level" {
			continue
		}
		if rv.Source != SourceFile {
			t.Errorf("log_level Source = %q, want %q", rv.Source, SourceFile)
		}
		if rv.FilePath == "" {
			t.Error("log_level FilePath should be non-empty when source is file")
		}
		if rv.EffectiveValue != "warn" {
			t.Errorf("log_level EffectiveValue = %q, want warn", rv.EffectiveValue)
		}
		return
	}
	t.Error("log_level not found in report fields")
}

// ── BuildResolveReport: secret redaction ─────────────────────────────────────

func TestBuildResolveReport_SecretsRedacted(t *testing.T) {
	orig := os.Getenv("GLASSBOX_RPC_TOKEN")
	t.Cleanup(func() { os.Setenv("GLASSBOX_RPC_TOKEN", orig) })
	os.Setenv("GLASSBOX_RPC_TOKEN", "supersecrettoken123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	report := BuildResolveReport(cfg, "")

	for _, rv := range report.Fields {
		if rv.Field != "rpc_token" {
			continue
		}
		if rv.EffectiveValue == "supersecrettoken123" {
			t.Error("rpc_token value must be redacted, not exposed in plain text")
		}
		if rv.EffectiveValue != "[redacted]" {
			t.Errorf("rpc_token EffectiveValue = %q, want [redacted]", rv.EffectiveValue)
		}
		if !rv.Redacted {
			t.Error("rpc_token Redacted field should be true")
		}
		return
	}
	t.Error("rpc_token not found in report — it should appear once it has a value")
}

func TestBuildResolveReport_CrashSentryDSNRedacted(t *testing.T) {
	orig := os.Getenv("GLASSBOX_SENTRY_DSN")
	t.Cleanup(func() { os.Setenv("GLASSBOX_SENTRY_DSN", orig) })
	os.Setenv("GLASSBOX_SENTRY_DSN", "https://key@o0.ingest.sentry.io/1")

	// Also need crash_reporting=true and crash_endpoint for validation to pass.
	origCR := os.Getenv("GLASSBOX_CRASH_REPORTING")
	t.Cleanup(func() { os.Setenv("GLASSBOX_CRASH_REPORTING", origCR) })
	os.Setenv("GLASSBOX_CRASH_REPORTING", "true")
	origCE := os.Getenv("GLASSBOX_CRASH_ENDPOINT")
	t.Cleanup(func() { os.Setenv("GLASSBOX_CRASH_ENDPOINT", origCE) })
	os.Setenv("GLASSBOX_CRASH_ENDPOINT", "https://crash.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	report := BuildResolveReport(cfg, "")

	for _, rv := range report.Fields {
		if rv.Field != "crash_sentry_dsn" {
			continue
		}
		if strings.Contains(rv.EffectiveValue, "key") {
			t.Error("crash_sentry_dsn must not expose the DSN key in the report")
		}
		if rv.EffectiveValue != "[redacted]" {
			t.Errorf("crash_sentry_dsn EffectiveValue = %q, want [redacted]", rv.EffectiveValue)
		}
		return
	}
	t.Error("crash_sentry_dsn not found in report when it has a value")
}

func TestBuildResolveReport_NetworkPassphraseRedacted(t *testing.T) {
	cfg := &Config{
		RpcUrl:            "https://rpc.example.com",
		Network:           NetworkTestnet,
		NetworkPassphrase: "Test SDF Network ; September 2015",
		RequestTimeout:    15,
		MaxTraceDepth:     50,
		FailureThreshold:  5,
		RetryTimeout:      60,
	}
	report := BuildResolveReport(cfg, "")

	for _, rv := range report.Fields {
		if rv.Field != "network_passphrase" {
			continue
		}
		if rv.EffectiveValue != "[redacted]" {
			t.Errorf("network_passphrase = %q, want [redacted]", rv.EffectiveValue)
		}
		return
	}
	t.Error("network_passphrase not found in report")
}

// ── BuildResolveReport: precedence and conflict notes ─────────────────────────

func TestBuildResolveReport_EnvOverridesFile_ProducesConflictNote(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	configPath := filepath.Join(project, ".glassbox.toml")
	if err := os.WriteFile(configPath, []byte(
		"rpc_url = \"https://file.example.com\"\nnetwork = \"testnet\"\nlog_level = \"info\"\n",
	), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	// Env var overrides the file value for log_level.
	origLogLevel := os.Getenv("GLASSBOX_LOG_LEVEL")
	t.Cleanup(func() { os.Setenv("GLASSBOX_LOG_LEVEL", origLogLevel) })
	os.Setenv("GLASSBOX_LOG_LEVEL", "debug")

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	report := BuildResolveReport(cfg, ActiveConfigFile())

	if len(report.ConflictNotes) == 0 {
		t.Error("expected at least one conflict note when env overrides a file value")
	}
	// At least one note should mention log_level.
	hasLogLevelNote := false
	for _, note := range report.ConflictNotes {
		if strings.Contains(note, "log_level") {
			hasLogLevelNote = true
		}
	}
	if !hasLogLevelNote {
		t.Errorf("conflict notes do not mention log_level: %v", report.ConflictNotes)
	}
}

// ── BuildResolveReport: JSON serialization ────────────────────────────────────

func TestBuildResolveReport_JSONRoundtrip(t *testing.T) {
	cfg := DefaultConfig()
	report := BuildResolveReport(cfg, "")

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded ResolveReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(decoded.Fields) != len(report.Fields) {
		t.Errorf("field count after round-trip: got %d, want %d", len(decoded.Fields), len(report.Fields))
	}
	for i := range report.Fields {
		if decoded.Fields[i].Field != report.Fields[i].Field {
			t.Errorf("field[%d] after round-trip: got %q, want %q", i, decoded.Fields[i].Field, report.Fields[i].Field)
		}
	}
}

func TestBuildResolveReport_JSONFieldNames_AreStable(t *testing.T) {
	cfg := DefaultConfig()
	report := BuildResolveReport(cfg, "")

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Verify the top-level field names are stable.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := raw["fields"]; !ok {
		t.Error("JSON output missing 'fields' key")
	}
}

// ── fileHasKey / resolveFileValue ─────────────────────────────────────────────

func TestFileHasKey_FindsExistingKey(t *testing.T) {
	f := writeTempTOML(t, "rpc_url = \"https://example.com\"\nnetwork = \"testnet\"\n")
	if !fileHasKey(f, "rpc_url") {
		t.Error("fileHasKey should find rpc_url")
	}
	if !fileHasKey(f, "network") {
		t.Error("fileHasKey should find network")
	}
}

func TestFileHasKey_MissingKey(t *testing.T) {
	f := writeTempTOML(t, "rpc_url = \"https://example.com\"\n")
	if fileHasKey(f, "log_level") {
		t.Error("fileHasKey should return false for absent key")
	}
}

func TestResolveFileValue_ReturnsValue(t *testing.T) {
	f := writeTempTOML(t, "rpc_url = \"https://file.example.com\"\n")
	got := resolveFileValue("rpc_url", f)
	if got != "https://file.example.com" {
		t.Errorf("resolveFileValue = %q, want https://file.example.com", got)
	}
}

func TestResolveFileValue_MissingKey_ReturnsEmpty(t *testing.T) {
	f := writeTempTOML(t, "rpc_url = \"https://file.example.com\"\n")
	got := resolveFileValue("log_level", f)
	if got != "" {
		t.Errorf("resolveFileValue for absent key = %q, want empty", got)
	}
}

// ── isBuiltinDefault ─────────────────────────────────────────────────────────

func TestIsBuiltinDefault_MatchesKnownDefaults(t *testing.T) {
	if !isBuiltinDefault("log_level", "info") {
		t.Error("log_level=info should be a builtin default")
	}
	if !isBuiltinDefault("network", "testnet") {
		t.Error("network=testnet should be a builtin default")
	}
}

func TestIsBuiltinDefault_NoMatchForCustomValue(t *testing.T) {
	if isBuiltinDefault("log_level", "debug") {
		t.Error("log_level=debug should NOT be the builtin default")
	}
}

func TestIsBuiltinDefault_UnknownField_ReturnsFalse(t *testing.T) {
	if isBuiltinDefault("unknown_field", "anything") {
		t.Error("unknown field should never be a default")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeTempTOML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.toml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}
