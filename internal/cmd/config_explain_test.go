// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotandev/glassbox/internal/config"
)

// ── --explain text mode ───────────────────────────────────────────────────────

func TestConfigShowExplain_TextMode_ContainsFieldTable(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	buf := &bytes.Buffer{}
	configShowCmd.SetOut(buf)
	configShowCmd.SetErr(&bytes.Buffer{})

	configShowJSONFlag = false
	configShowExplainFlag = true
	t.Cleanup(func() { configShowExplainFlag = false })

	if err := configShowCmd.RunE(configShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "FIELD") {
		t.Error("explain text output should contain 'FIELD' column header")
	}
	if !strings.Contains(output, "SOURCE") {
		t.Error("explain text output should contain 'SOURCE' column header")
	}
	if !strings.Contains(output, "EFFECTIVE VALUE") {
		t.Error("explain text output should contain 'EFFECTIVE VALUE' column header")
	}
	// At least one field row must be present.
	if !strings.Contains(output, "rpc_url") && !strings.Contains(output, "network") {
		t.Error("explain output should include at least rpc_url or network rows")
	}
}

func TestConfigShowExplain_TextMode_ShowsDefaultLabel(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	// Clear env vars that would override defaults.
	for _, k := range []string{"GLASSBOX_LOG_LEVEL", "GLASSBOX_NETWORK", "GLASSBOX_RPC_URL"} {
		orig := os.Getenv(k)
		os.Unsetenv(k)
		t.Cleanup(func() { os.Setenv(k, orig) })
	}

	buf := &bytes.Buffer{}
	configShowCmd.SetOut(buf)
	configShowCmd.SetErr(&bytes.Buffer{})

	configShowJSONFlag = false
	configShowExplainFlag = true
	t.Cleanup(func() { configShowExplainFlag = false })

	if err := configShowCmd.RunE(configShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	output := buf.String()
	// Default-sourced fields include "(default)" annotation.
	if !strings.Contains(output, "(default)") {
		t.Error("explain text output should label default-applied fields with '(default)'")
	}
}

func TestConfigShowExplain_TextMode_ShowsEnvLabel(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	orig := os.Getenv("GLASSBOX_LOG_LEVEL")
	t.Cleanup(func() { os.Setenv("GLASSBOX_LOG_LEVEL", orig) })
	os.Setenv("GLASSBOX_LOG_LEVEL", "debug")

	buf := &bytes.Buffer{}
	configShowCmd.SetOut(buf)
	configShowCmd.SetErr(&bytes.Buffer{})

	configShowJSONFlag = false
	configShowExplainFlag = true
	t.Cleanup(func() { configShowExplainFlag = false })

	if err := configShowCmd.RunE(configShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "environment") {
		t.Error("explain output should show 'environment' source label")
	}
	if !strings.Contains(output, "GLASSBOX_LOG_LEVEL") {
		t.Error("explain output should show the env var name")
	}
}

// ── --explain --json mode ─────────────────────────────────────────────────────

func TestConfigShowExplain_JSONMode_StableFieldNames(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	buf := &bytes.Buffer{}
	configShowCmd.SetOut(buf)
	configShowCmd.SetErr(&bytes.Buffer{})

	configShowJSONFlag = true
	configShowExplainFlag = true
	t.Cleanup(func() {
		configShowJSONFlag = false
		configShowExplainFlag = false
	})

	if err := configShowCmd.RunE(configShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var report config.ResolveReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("JSON unmarshal: %v\noutput: %s", err, buf.String())
	}

	if len(report.Fields) == 0 {
		t.Error("explain JSON output must have at least one field")
	}

	// Verify the stable top-level keys.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(buf.Bytes(), &raw)
	if _, ok := raw["fields"]; !ok {
		t.Error("explain JSON missing 'fields' key")
	}
}

func TestConfigShowExplain_JSONMode_SecretsNotInPlainText(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	orig := os.Getenv("GLASSBOX_RPC_TOKEN")
	t.Cleanup(func() { os.Setenv("GLASSBOX_RPC_TOKEN", orig) })
	os.Setenv("GLASSBOX_RPC_TOKEN", "SECRETTOKEN999")

	buf := &bytes.Buffer{}
	configShowCmd.SetOut(buf)
	configShowCmd.SetErr(&bytes.Buffer{})

	configShowJSONFlag = true
	configShowExplainFlag = true
	t.Cleanup(func() {
		configShowJSONFlag = false
		configShowExplainFlag = false
	})

	if err := configShowCmd.RunE(configShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rawOutput := buf.String()
	if strings.Contains(rawOutput, "SECRETTOKEN999") {
		t.Error("explain JSON output must not contain the plain-text secret token")
	}
	if !strings.Contains(rawOutput, "[redacted]") {
		t.Error("explain JSON output should contain '[redacted]' placeholder for the secret")
	}
}

func TestConfigShowExplain_JSONMode_PrecedenceOrder(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	// File sets log_level=warn; env will override it to debug.
	configPath := filepath.Join(project, ".glassbox.toml")
	if err := os.WriteFile(configPath, []byte(
		"rpc_url = \"https://file.example.com\"\nnetwork = \"testnet\"\nlog_level = \"warn\"\n",
	), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	orig := os.Getenv("GLASSBOX_LOG_LEVEL")
	t.Cleanup(func() { os.Setenv("GLASSBOX_LOG_LEVEL", orig) })
	os.Setenv("GLASSBOX_LOG_LEVEL", "debug")

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	_ = os.Chdir(project)

	buf := &bytes.Buffer{}
	configShowCmd.SetOut(buf)
	configShowCmd.SetErr(&bytes.Buffer{})

	configShowJSONFlag = true
	configShowExplainFlag = true
	t.Cleanup(func() {
		configShowJSONFlag = false
		configShowExplainFlag = false
	})

	if err := configShowCmd.RunE(configShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var report config.ResolveReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}

	for _, rv := range report.Fields {
		if rv.Field != "log_level" {
			continue
		}
		// Env beats file — source must be "environment".
		if rv.Source != config.SourceEnvironment {
			t.Errorf("log_level source = %q, want environment (env overrides file)", rv.Source)
		}
		if rv.EffectiveValue != "debug" {
			t.Errorf("log_level effective_value = %q, want debug", rv.EffectiveValue)
		}
		return
	}
	t.Error("log_level field not found in explain JSON output")
}

// ── backward-compatibility: --explain absent behaves like before ──────────────

func TestConfigShowExplain_FlagAbsent_DefaultOutputUnchanged(t *testing.T) {
	home := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", home)

	buf := &bytes.Buffer{}
	configShowCmd.SetOut(buf)
	configShowCmd.SetErr(&bytes.Buffer{})

	configShowJSONFlag = false
	configShowExplainFlag = false

	if err := configShowCmd.RunE(configShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	output := buf.String()
	// Original output must still contain "Active configuration:" — the
	// explain flag must not change default behavior.
	if !strings.Contains(output, "Active configuration:") {
		t.Error("default output (no --explain) should still contain 'Active configuration:'")
	}
}
