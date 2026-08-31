// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package clioutput – error output helpers.
//
// ErrorEnvelope is the stable JSON shape emitted by every Glassbox command
// when an error occurs in JSON mode.  The shape is guaranteed across all
// migrated commands so that automation can parse failures reliably.
//
//	{
//	  "schema_version": "1.0",
//	  "glassbox_version": "...",
//	  "generated_at":    "...",
//	  "command":         "debug",          // omitted when unknown
//	  "error": {
//	    "code":        "RPC_CONNECTION_FAILED",
//	    "severity":    "error",
//	    "message":     "RPC connection failed: dial tcp ...",
//	    "remediation": "Check your internet connection ...",
//	    "context":     { ... }              // optional, command-specific
//	  }
//	}
package clioutput

import (
	stderrors "errors"
	"fmt"
	"io"
	"os"

	glassboxerrors "github.com/dotandev/glassbox/internal/errors"
)

// Severity classifies how bad an error is.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// ErrorPayload is the stable, machine-readable shape for an error.
// Every field except Context is required in the output.
type ErrorPayload struct {
	// Code is the stable ErstErrorCode string (e.g. "RPC_CONNECTION_FAILED").
	// Automation should key on this field, not on the human-readable Message.
	Code string `json:"code"`

	// Severity is currently always "error" for command failures.
	Severity Severity `json:"severity"`

	// Message is the full human-readable error string.  It may change between
	// releases; automation should use Code instead.
	Message string `json:"message"`

	// Remediation is an optional actionable hint explaining how to recover.
	// Present only when the error carries a Hint.
	Remediation string `json:"remediation,omitempty"`

	// Context holds optional, command-specific key/value metadata such as the
	// transaction hash, network name, or file path that caused the failure.
	Context map[string]string `json:"context,omitempty"`
}

// errorEnvelopeData is the full envelope used when marshaling error output.
type errorEnvelopeData struct {
	Error ErrorPayload `json:"error"`
}

// errorCodeOf extracts the ErstErrorCode string from err, or returns "UNKNOWN".
func errorCodeOf(err error) string {
	var e *glassboxerrors.ErstError
	if stderrors.As(err, &e) {
		return string(e.Code)
	}
	return string(glassboxerrors.ErstUnknown)
}

// WriteError writes a structured error envelope to w in JSON mode.
// It mirrors Write() but wraps the error under an "error" key so consumers can
// distinguish error envelopes from data envelopes by checking that key.
//
// ctx may be nil; when non-nil its key/value pairs are added to the error's
// Context map for additional diagnostic detail.
func WriteError(w io.Writer, command string, err error, ctx map[string]string) error {
	if err == nil {
		return nil
	}
	payload := ErrorPayload{
		Code:        errorCodeOf(err),
		Severity:    SeverityError,
		Message:     err.Error(),
		Remediation: glassboxerrors.Hint(err),
		Context:     ctx,
	}
	return Write(w, command, errorEnvelopeData{Error: payload})
}

// WriteErrorStdout is a convenience wrapper around WriteError(os.Stdout, ...).
func WriteErrorStdout(command string, err error, ctx map[string]string) error {
	return WriteError(os.Stdout, command, err, ctx)
}

// FormatErrorText returns the human-readable text representation of an error
// for terminal output.  It includes the stable error code, the message, and the
// remediation hint (if any) so the text and JSON modes carry the same
// information.
//
//	[RPC_CONNECTION_FAILED] RPC connection failed: dial tcp …
//	Hint: Check your internet connection …
func FormatErrorText(err error) string {
	if err == nil {
		return ""
	}
	code := errorCodeOf(err)
	msg := fmt.Sprintf("[%s] %s", code, err.Error())
	if hint := glassboxerrors.Hint(err); hint != "" {
		msg += "\nHint: " + hint
	}
	return msg
}


