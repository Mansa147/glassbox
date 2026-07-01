// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package protocolreg

import (
	"strings"
	"testing"
)

func TestDiagnosticReport_Summary_OK(t *testing.T) {
	report := &DiagnosticReport{
		Platform: "linux",
		Scheme:   Scheme,
		Status:   StatusOK,
	}
	s := report.Summary()
	if !strings.Contains(s, "healthy") {
		t.Errorf("Summary() should mention healthy state, got: %q", s)
	}
	if !strings.Contains(s, Scheme+"://") {
		t.Errorf("Summary() should include scheme, got: %q", s)
	}
}

func TestDiagnosticReport_Summary_NotRegistered(t *testing.T) {
	report := &DiagnosticReport{
		Platform: "darwin",
		Scheme:   Scheme,
		Status:   StatusNotRegistered,
		Issues:   []string{"app bundle plist not found"},
	}
	s := report.Summary()
	if !strings.Contains(s, "not registered") {
		t.Errorf("Summary() should mention not registered, got: %q", s)
	}
	if !strings.Contains(s, "1 issue") {
		t.Errorf("Summary() should include issue count, got: %q", s)
	}
}

func TestDiagnosticReport_Summary_Degraded(t *testing.T) {
	report := &DiagnosticReport{
		Platform: "linux",
		Scheme:   Scheme,
		Status:   StatusDegraded,
		Issues:   []string{"stale path", "xdg-mime mismatch"},
	}
	s := report.Summary()
	if !strings.Contains(s, "degraded") {
		t.Errorf("Summary() should mention degraded, got: %q", s)
	}
	if !strings.Contains(s, "2 issue") {
		t.Errorf("Summary() should include issue count, got: %q", s)
	}
}

func TestDiagnosticReport_Summary_Error(t *testing.T) {
	report := &DiagnosticReport{
		Platform: "freebsd",
		Scheme:   Scheme,
		Status:   StatusError,
		Issues:   []string{"unsupported platform"},
	}
	s := report.Summary()
	if !strings.Contains(s, "unsupported platform") {
		t.Errorf("Summary() should include issue text, got: %q", s)
	}
}

func TestDiagnosticReport_Summary_NilReport(t *testing.T) {
	var report *DiagnosticReport
	if s := report.Summary(); s == "" {
		t.Error("Summary() on nil report should return a non-empty fallback")
	}
}

func TestVerificationReport_Summary_Passed(t *testing.T) {
	report := &VerificationReport{
		Platform: "linux",
		Scheme:   Scheme,
		Checks:   []string{"desktop file found", "wrapper script ok"},
	}
	s := report.Summary()
	if !strings.Contains(s, "Verified") {
		t.Errorf("Summary() should mention verification success, got: %q", s)
	}
	if !strings.Contains(s, "2 check") {
		t.Errorf("Summary() should include check count, got: %q", s)
	}
}

func TestVerificationReport_Summary_Failed(t *testing.T) {
	report := &VerificationReport{
		Platform: "windows",
		Scheme:   Scheme,
		Checks:   []string{"registry key exists"},
		Issues:   []string{"missing URL Protocol value", "open command mismatch"},
	}
	s := report.Summary()
	if !strings.Contains(s, "failed") {
		t.Errorf("Summary() should mention failure, got: %q", s)
	}
	if !strings.Contains(s, "2 issue") {
		t.Errorf("Summary() should include issue count, got: %q", s)
	}
}

func TestVerificationReport_Summary_NilReport(t *testing.T) {
	var report *VerificationReport
	if s := report.Summary(); s == "" {
		t.Error("Summary() on nil report should return a non-empty fallback")
	}
}
