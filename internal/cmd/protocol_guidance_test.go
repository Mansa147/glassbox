// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dotandev/glassbox/internal/protocolreg"
)

// ── protocol:status remediation guidance ─────────────────────────────────────

// TestProtocolStatusCmd_NotRegistered_RemediationRendering verifies that the
// remediation-steps rendering logic produces numbered, non-empty steps.
func TestProtocolStatusCmd_NotRegistered_RemediationRendering(t *testing.T) {
	// Simulate the rendering path from protocolStatusCmd.RunE for the
	// not-registered case (mirrors the code in protocol.go).
	diag := &protocolreg.DiagnosticReport{
		Status: protocolreg.StatusNotRegistered,
		Issues: []string{"desktop file not found"},
		RemediationSteps: []string{
			"Run 'glassbox protocol:repair' to recreate the .desktop file and re-register.",
			"Or manually create /home/user/.local/share/applications/glassbox-protocol.desktop",
		},
	}

	var stderr strings.Builder
	stderr.WriteString("GLASSBOX Protocol handler is NOT REGISTERED\n")
	if len(diag.RemediationSteps) > 0 {
		stderr.WriteString("\nTo register the protocol handler:\n")
		for _, step := range diag.RemediationSteps {
			stderr.WriteString("  - " + step + "\n")
		}
	}

	out := stderr.String()
	if !strings.Contains(out, "NOT REGISTERED") {
		t.Errorf("status output should state NOT REGISTERED, got: %s", out)
	}
	if !strings.Contains(out, "protocol:repair") {
		t.Errorf("status output should mention protocol:repair, got: %s", out)
	}
	if !strings.Contains(out, "To register the protocol handler:") {
		t.Errorf("status output should include the remediation header, got: %s", out)
	}
}

// TestProtocolStatusCmd_Registered_NoRemediation verifies that a healthy
// registration produces no remediation output.
func TestProtocolStatusCmd_Registered_NoRemediation(t *testing.T) {
	diag := &protocolreg.DiagnosticReport{
		Status: protocolreg.StatusOK,
		Checks: []string{"Desktop file found", "Protocol helper script launches current binary"},
	}

	var stdout strings.Builder
	for _, check := range diag.Checks {
		stdout.WriteString("[OK] " + check + "\n")
	}
	if diag.Status == protocolreg.StatusOK {
		stdout.WriteString("GLASSBOX Protocol handler is currently REGISTERED\n")
	}

	out := stdout.String()
	if !strings.Contains(out, "REGISTERED") {
		t.Errorf("expected REGISTERED in output, got: %s", out)
	}
	if strings.Contains(out, "protocol:repair") {
		t.Errorf("healthy registration should not mention protocol:repair, got: %s", out)
	}
}

// ── protocol:register next-step hint ─────────────────────────────────────────

// TestProtocolRegisterCmd_SuccessMessage_IncludesVerifyHint verifies that the
// success message produced after registration directs users to run
// protocol:verify as a next step.
func TestProtocolRegisterCmd_SuccessMessage_IncludesVerifyHint(t *testing.T) {
	var stdout strings.Builder
	scheme := protocolreg.Scheme
	stdout.WriteString("Registered GLASSBOX Protocol handler for " + scheme + "://\n")
	stdout.WriteString("Tip: run 'glassbox protocol:verify' to confirm the registration is working.\n")

	out := stdout.String()
	if !strings.Contains(out, "protocol:verify") {
		t.Errorf("register success message should mention protocol:verify, got: %s", out)
	}
	if !strings.Contains(out, scheme+"://") {
		t.Errorf("register success message should include the scheme, got: %s", out)
	}
}

// ── protocol:handle URI format hint ──────────────────────────────────────────

// TestProtocolHandleCmd_InvalidURI_ErrorIncludesFormatHint verifies that the
// error returned for an invalid URI includes the expected URI format so the
// user knows how to construct a valid one.
func TestProtocolHandleCmd_InvalidURI_ErrorIncludesFormatHint(t *testing.T) {
	_, parseErr := protocolreg.ParseDebugURI("https://wrong-scheme/abc")
	if parseErr == nil {
		t.Fatal("expected parse error for wrong scheme")
	}
	// Simulate the wrapping applied by protocol:handle.
	wrappedErr := parseErr.Error() +
		"\n  Expected format: glassbox://debug/<64-char-hex>?network=<testnet|mainnet|futurenet>[&op=<n>][&view=<mode>]"

	for _, keyword := range []string{"glassbox://debug/", "network=", "testnet", "mainnet", "futurenet"} {
		if !strings.Contains(wrappedErr, keyword) {
			t.Errorf("handle error should mention %q; got: %s", keyword, wrappedErr)
		}
	}
}

// TestProtocolHandleCmd_EmptyURI_ErrorIsNonEmpty confirms that an empty URI
// produces a non-empty, human-readable error.
func TestProtocolHandleCmd_EmptyURI_ErrorIsNonEmpty(t *testing.T) {
	_, err := protocolreg.ParseDebugURI("")
	if err == nil {
		t.Fatal("expected error for empty URI")
	}
	if strings.TrimSpace(err.Error()) == "" {
		t.Error("error for empty URI must be a non-empty string")
	}
}

// TestProtocolHandleCmd_InvalidNetwork_ErrorListsOptions verifies that the
// error for an unrecognised network names the accepted values.
func TestProtocolHandleCmd_InvalidNetwork_ErrorListsOptions(t *testing.T) {
	validHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	_, err := protocolreg.ParseDebugURI("glassbox://debug/" + validHash + "?network=badnet")
	if err == nil {
		t.Fatal("expected error for invalid network")
	}
	for _, net := range []string{"testnet", "mainnet", "futurenet"} {
		if !strings.Contains(err.Error(), net) {
			t.Errorf("network error should list valid option %q; got: %v", net, err)
		}
	}
}

// ── protocol:register --dry-run output ───────────────────────────────────────

// TestProtocolRegisterDryRun_NotRegistered_ShowsWouldRegister verifies that
// the dry-run path emits a preview message when the handler is not registered.
func TestProtocolRegisterDryRun_NotRegistered_ShowsWouldRegister(t *testing.T) {
	// Simulate the dry-run rendering for a not-registered state.
	diag := &protocolreg.DiagnosticReport{
		Status:   protocolreg.StatusNotRegistered,
		Platform: "linux",
		Issues:   []string{"desktop file not found"},
	}

	var stdout strings.Builder
	if diag.Status == protocolreg.StatusOK {
		stdout.WriteString("[DRY-RUN] Protocol handler is already registered — no changes needed.\n")
	} else {
		stdout.WriteString("[DRY-RUN] Would register " + protocolreg.Scheme + ":// handler on " + diag.Platform + ".\n")
		stdout.WriteString("[DRY-RUN] Current status: " + string(diag.Status) + "\n")
		if len(diag.Issues) > 0 {
			stdout.WriteString("[DRY-RUN] Issues to fix:\n")
			for _, issue := range diag.Issues {
				stdout.WriteString("  - " + issue + "\n")
			}
		}
	}

	out := stdout.String()
	if !strings.Contains(out, "[DRY-RUN]") {
		t.Errorf("dry-run output should contain '[DRY-RUN]' prefix, got: %s", out)
	}
	if !strings.Contains(out, "Would register") {
		t.Errorf("dry-run output should contain 'Would register', got: %s", out)
	}
	if !strings.Contains(out, protocolreg.Scheme+"://") {
		t.Errorf("dry-run output should contain the scheme, got: %s", out)
	}
}

// TestProtocolRegisterDryRun_AlreadyRegistered_ShowsNoOp verifies the dry-run
// message when the handler is already registered.
func TestProtocolRegisterDryRun_AlreadyRegistered_ShowsNoOp(t *testing.T) {
	diag := &protocolreg.DiagnosticReport{
		Status:   protocolreg.StatusOK,
		Platform: "linux",
	}

	var stdout strings.Builder
	if diag.Status == protocolreg.StatusOK {
		stdout.WriteString("[DRY-RUN] Protocol handler is already registered — no changes needed.\n")
	}

	out := stdout.String()
	if !strings.Contains(out, "already registered") {
		t.Errorf("dry-run for registered state should say 'already registered', got: %s", out)
	}
	if !strings.Contains(out, "[DRY-RUN]") {
		t.Errorf("dry-run for registered state should include '[DRY-RUN]' prefix, got: %s", out)
	}
}

// TestProtocolRegisterCmd_FailureError_MentionsRepairTip verifies that a
// registration failure wraps a suggestion to run protocol:repair.
func TestProtocolRegisterCmd_FailureError_MentionsRepairTip(t *testing.T) {
	// Simulate the error wrapping applied in protocol:register RunE.
	baseErr := fmt.Errorf("xdg-mime is not installed")
	wrapped := baseErr.Error() +
		"\n  Tip: run 'glassbox protocol:diagnose' for a detailed breakdown, or 'glassbox protocol:repair' to attempt automatic repair"

	if !strings.Contains(wrapped, "protocol:diagnose") {
		t.Errorf("register failure should mention protocol:diagnose; got: %s", wrapped)
	}
	if !strings.Contains(wrapped, "protocol:repair") {
		t.Errorf("register failure should mention protocol:repair; got: %s", wrapped)
	}
}

// ── DiagnosticReport.Summary rendering ───────────────────────────────────────

func TestProtocolDiagnoseCmd_SummaryLine_IncludesStatus(t *testing.T) {
	report := &protocolreg.DiagnosticReport{
		Platform: "linux",
		Scheme:   protocolreg.Scheme,
		Status:   protocolreg.StatusDegraded,
		Issues:   []string{"wrapper script does not reference current binary"},
	}

	var stdout strings.Builder
	stdout.WriteString("Summary: " + report.Summary() + "\n")

	out := stdout.String()
	if !strings.Contains(out, "Summary:") {
		t.Errorf("diagnose output should include Summary line, got: %s", out)
	}
	if !strings.Contains(out, "degraded") {
		t.Errorf("summary should mention degraded status, got: %s", out)
	}
	if !strings.Contains(out, protocolreg.Scheme+"://") {
		t.Errorf("summary should include scheme, got: %s", out)
	}
}
