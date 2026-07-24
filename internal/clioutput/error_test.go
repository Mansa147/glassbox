// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package clioutput

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	glassboxerrors "github.com/dotandev/glassbox/internal/errors"
)

// ── errorCodeOf ───────────────────────────────────────────────────────────────

func TestErrorCodeOf_ErstError(t *testing.T) {
	err := glassboxerrors.WrapValidationError("bad input")
	got := errorCodeOf(err)
	if got != string(glassboxerrors.ErstValidationFailed) {
		t.Errorf("errorCodeOf(validation err) = %q, want %q", got, glassboxerrors.ErstValidationFailed)
	}
}

func TestErrorCodeOf_ErstRPCConnectionFailed(t *testing.T) {
	err := glassboxerrors.WrapRPCConnectionFailed(fmt.Errorf("dial failed"))
	got := errorCodeOf(err)
	if got != string(glassboxerrors.ErstRPCConnectionFailed) {
		t.Errorf("errorCodeOf(rpc err) = %q, want %q", got, glassboxerrors.ErstRPCConnectionFailed)
	}
}

func TestErrorCodeOf_PlainError_ReturnsUnknown(t *testing.T) {
	got := errorCodeOf(fmt.Errorf("something went wrong"))
	if got != string(glassboxerrors.ErstUnknown) {
		t.Errorf("errorCodeOf(plain err) = %q, want %q", got, glassboxerrors.ErstUnknown)
	}
}

func TestErrorCodeOf_ConfigError(t *testing.T) {
	err := glassboxerrors.WrapConfigError("bad config", fmt.Errorf("missing rpc_url"))
	got := errorCodeOf(err)
	if got != string(glassboxerrors.ErstConfigFailed) {
		t.Errorf("errorCodeOf(config err) = %q, want %q", got, glassboxerrors.ErstConfigFailed)
	}
}

// ── WriteError ────────────────────────────────────────────────────────────────

func TestWriteError_ProducesStableCodeField(t *testing.T) {
	var buf bytes.Buffer
	err := glassboxerrors.WrapRPCConnectionFailed(fmt.Errorf("dial tcp: refused"))

	if writeErr := WriteError(&buf, "debug", err, nil); writeErr != nil {
		t.Fatalf("WriteError returned unexpected error: %v", writeErr)
	}

	// Unmarshal the outer envelope.
	var env Envelope
	if jsonErr := json.Unmarshal(buf.Bytes(), &env); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jsonErr, buf.String())
	}
	if env.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %q, want %q", env.SchemaVersion, SchemaVersion)
	}
	if env.Command != "debug" {
		t.Errorf("command = %q, want %q", env.Command, "debug")
	}

	// Unmarshal the data payload.
	var data errorEnvelopeData
	if jsonErr := json.Unmarshal(env.Data, &data); jsonErr != nil {
		t.Fatalf("data is not valid JSON: %v", jsonErr)
	}
	if data.Error.Code != string(glassboxerrors.ErstRPCConnectionFailed) {
		t.Errorf("error.code = %q, want %q", data.Error.Code, glassboxerrors.ErstRPCConnectionFailed)
	}
	if data.Error.Severity != SeverityError {
		t.Errorf("error.severity = %q, want %q", data.Error.Severity, SeverityError)
	}
	if data.Error.Message == "" {
		t.Error("error.message must not be empty")
	}
}

func TestWriteError_RemediationIncluded_WhenHintPresent(t *testing.T) {
	var buf bytes.Buffer
	err := glassboxerrors.WrapRPCTimeout(fmt.Errorf("context deadline exceeded"))

	if writeErr := WriteError(&buf, "debug", err, nil); writeErr != nil {
		t.Fatalf("WriteError: %v", writeErr)
	}

	var env Envelope
	_ = json.Unmarshal(buf.Bytes(), &env)
	var data errorEnvelopeData
	_ = json.Unmarshal(env.Data, &data)

	if data.Error.Remediation == "" {
		t.Error("error.remediation should be populated from hint when hint is present")
	}
}

func TestWriteError_NoRemediationWhenNoHint(t *testing.T) {
	var buf bytes.Buffer
	err := glassboxerrors.WrapValidationError("rpc_url cannot be empty")

	if writeErr := WriteError(&buf, "config", err, nil); writeErr != nil {
		t.Fatalf("WriteError: %v", writeErr)
	}

	var env Envelope
	_ = json.Unmarshal(buf.Bytes(), &env)
	var data errorEnvelopeData
	_ = json.Unmarshal(env.Data, &data)

	// Validation errors carry no hint by default; remediation should be absent.
	if data.Error.Remediation != "" {
		t.Errorf("error.remediation should be empty, got %q", data.Error.Remediation)
	}
}

func TestWriteError_ContextIncluded(t *testing.T) {
	var buf bytes.Buffer
	err := glassboxerrors.WrapTransactionNotFound(fmt.Errorf("404"))
	ctx := map[string]string{"tx_hash": "abc123", "network": "testnet"}

	if writeErr := WriteError(&buf, "debug", err, ctx); writeErr != nil {
		t.Fatalf("WriteError: %v", writeErr)
	}

	var env Envelope
	_ = json.Unmarshal(buf.Bytes(), &env)
	var data errorEnvelopeData
	_ = json.Unmarshal(env.Data, &data)

	if data.Error.Context["tx_hash"] != "abc123" {
		t.Errorf("context.tx_hash = %q, want %q", data.Error.Context["tx_hash"], "abc123")
	}
	if data.Error.Context["network"] != "testnet" {
		t.Errorf("context.network = %q, want %q", data.Error.Context["network"], "testnet")
	}
}

func TestWriteError_NilError_WritesNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteError(&buf, "debug", nil, nil); err != nil {
		t.Fatalf("WriteError(nil) returned error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("WriteError(nil) wrote %d bytes, want 0", buf.Len())
	}
}

// ── FormatErrorText ───────────────────────────────────────────────────────────

func TestFormatErrorText_IncludesCode(t *testing.T) {
	err := glassboxerrors.WrapValidationError("rpc_url cannot be empty")
	text := FormatErrorText(err)
	if text == "" {
		t.Fatal("FormatErrorText returned empty string")
	}
	// Must contain the stable code prefix.
	if !contains(text, "[VALIDATION_FAILED]") {
		t.Errorf("FormatErrorText output missing code prefix: %q", text)
	}
}

func TestFormatErrorText_IncludesHint_WhenPresent(t *testing.T) {
	err := glassboxerrors.WrapRPCConnectionFailed(fmt.Errorf("no route to host"))
	text := FormatErrorText(err)
	if !contains(text, "Hint:") {
		t.Errorf("FormatErrorText should include hint line; got: %q", text)
	}
}

func TestFormatErrorText_NoHintLine_WhenAbsent(t *testing.T) {
	err := glassboxerrors.WrapValidationError("simulator_path must be absolute")
	text := FormatErrorText(err)
	if contains(text, "Hint:") {
		t.Errorf("FormatErrorText should not include hint line when hint is absent; got: %q", text)
	}
}

func TestFormatErrorText_PlainError_UsesUnknownCode(t *testing.T) {
	err := fmt.Errorf("some unexpected failure")
	text := FormatErrorText(err)
	if !contains(text, "[UNKNOWN]") {
		t.Errorf("FormatErrorText for plain error should use [UNKNOWN] prefix; got: %q", text)
	}
}

func TestFormatErrorText_NilError_ReturnsEmpty(t *testing.T) {
	if got := FormatErrorText(nil); got != "" {
		t.Errorf("FormatErrorText(nil) = %q, want empty", got)
	}
}

// ── TextAndJSONCodeConsistency ────────────────────────────────────────────────

// TestTextAndJSONModeEmitSameCode verifies that the stable error code is
// identical whether the error is rendered as text or JSON — satisfying the
// "same stable code in text and JSON modes" acceptance criterion.
func TestTextAndJSONModeEmitSameCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "validation error",
			err:      glassboxerrors.WrapValidationError("rpc_url cannot be empty"),
			wantCode: "VALIDATION_FAILED",
		},
		{
			name:     "rpc connection failed (runtime error)",
			err:      glassboxerrors.WrapRPCConnectionFailed(fmt.Errorf("connect refused")),
			wantCode: "RPC_CONNECTION_FAILED",
		},
		{
			name:     "config error",
			err:      glassboxerrors.WrapConfigError("missing field", fmt.Errorf("rpc_url empty")),
			wantCode: "CONFIG_ERROR",
		},
		{
			name:     "transaction not found",
			err:      glassboxerrors.WrapTransactionNotFound(fmt.Errorf("404")),
			wantCode: "TRANSACTION_NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Text mode: code extracted by FormatErrorText
			textOut := FormatErrorText(tt.err)
			wantPrefix := "[" + tt.wantCode + "]"
			if !contains(textOut, wantPrefix) {
				t.Errorf("text mode: output %q does not contain code prefix %q", textOut, wantPrefix)
			}

			// JSON mode: code in error.code field
			var buf bytes.Buffer
			_ = WriteError(&buf, "test", tt.err, nil)
			var env Envelope
			_ = json.Unmarshal(buf.Bytes(), &env)
			var data errorEnvelopeData
			_ = json.Unmarshal(env.Data, &data)
			if data.Error.Code != tt.wantCode {
				t.Errorf("JSON mode: error.code = %q, want %q", data.Error.Code, tt.wantCode)
			}
		})
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
