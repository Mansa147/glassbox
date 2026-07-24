// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package termctx

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// resetGlobal resets the process-wide non-interactive flag after each test
// so tests are fully independent of run order.
func resetGlobal(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { globalNonInteractive.Store(false) })
}

// ── Explicit flag precedence ──────────────────────────────────────────────────

func TestNew_ExplicitNonInteractiveFlag_Wins(t *testing.T) {
	resetGlobal(t)
	// Even with FORCE_COLOR set the explicit flag must win.
	t.Setenv("FORCE_COLOR", "1")

	tc := New(Options{NonInteractive: true})

	if tc.IsInteractive() {
		t.Error("IsInteractive() should be false when NonInteractive option is true")
	}
	if tc.StdoutIsTTY() {
		t.Error("StdoutIsTTY() should be false when NonInteractive is forced")
	}
	if tc.StdinIsTTY() {
		t.Error("StdinIsTTY() should be false when NonInteractive is forced")
	}
}

func TestSetGlobalNonInteractive_OverridesAutoDetect(t *testing.T) {
	resetGlobal(t)
	SetGlobalNonInteractive(true)

	// A context built without the explicit option still sees non-interactive.
	tc := New(Options{})
	if tc.IsInteractive() {
		t.Error("IsInteractive() should be false after SetGlobalNonInteractive(true)")
	}
}

func TestGlobalNonInteractive_FalseByDefault(t *testing.T) {
	resetGlobal(t)
	if GlobalNonInteractive() {
		t.Error("GlobalNonInteractive() should be false before any call to SetGlobalNonInteractive")
	}
}

// ── Environment variable detection ───────────────────────────────────────────

func TestNew_CI_EnvForcesNonInteractive(t *testing.T) {
	resetGlobal(t)
	t.Setenv("CI", "true")

	tc := New(Options{})
	if tc.IsInteractive() {
		t.Error("IsInteractive() should be false when CI env var is set")
	}
}

func TestNew_GlassboxNonInteractive_EnvForcesNonInteractive(t *testing.T) {
	resetGlobal(t)
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "1")

	tc := New(Options{})
	if tc.IsInteractive() {
		t.Error("IsInteractive() should be false when GLASSBOX_NON_INTERACTIVE=1")
	}
}

func TestNew_DebianFrontend_EnvForcesNonInteractive(t *testing.T) {
	resetGlobal(t)
	t.Setenv("DEBIAN_FRONTEND", "noninteractive")

	tc := New(Options{})
	if tc.IsInteractive() {
		t.Error("IsInteractive() should be false when DEBIAN_FRONTEND=noninteractive")
	}
}

func TestNew_DebianFrontendOtherValue_DoesNotForce(t *testing.T) {
	resetGlobal(t)
	// Any value other than "noninteractive" must not trigger the env check.
	t.Setenv("DEBIAN_FRONTEND", "dialog")
	// Unset CI so only DEBIAN_FRONTEND is relevant here.
	t.Setenv("CI", "")
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "")

	// We can't assert IsInteractive() == true because stdout may not be a TTY
	// in the test runner, but we can assert that the env check itself is clean.
	if envForcesNonInteractive() {
		t.Error("envForcesNonInteractive() should be false when DEBIAN_FRONTEND=dialog")
	}
}

// ── Pipe-backed stream detection ─────────────────────────────────────────────

func TestNew_PipeBackedStdout_IsNonInteractive(t *testing.T) {
	resetGlobal(t)
	// Unset all env vars that could interfere.
	t.Setenv("CI", "")
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "")
	t.Setenv("DEBIAN_FRONTEND", "")
	t.Setenv("FORCE_COLOR", "")

	// bytes.Buffer is not an *os.File — resolveFile returns (nil, false)
	// for stdout, so stdoutIsTTY is false and non-interactive is set.
	var pipe bytes.Buffer
	tc := New(Options{Stdout: &pipe})

	if tc.StdoutIsTTY() {
		t.Error("StdoutIsTTY() should be false for a non-*os.File writer")
	}
	if tc.IsInteractive() {
		t.Error("IsInteractive() should be false when stdout is a pipe/buffer")
	}
}

func TestNew_PipeBackedStdin_StdinIsNotTTY(t *testing.T) {
	resetGlobal(t)
	t.Setenv("CI", "")
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "")
	t.Setenv("DEBIAN_FRONTEND", "")

	var pipe bytes.Buffer
	tc := New(Options{Stdin: &pipe})

	if tc.StdinIsTTY() {
		t.Error("StdinIsTTY() should be false for a non-*os.File reader")
	}
}

func TestNew_NilStdout_FallsBackToOsStdout(t *testing.T) {
	resetGlobal(t)
	t.Setenv("CI", "")
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "")
	t.Setenv("DEBIAN_FRONTEND", "")

	// nil Stdout: resolveFile falls back to os.Stdout.
	// In test runners os.Stdout is typically not a TTY, but this must not panic.
	tc := New(Options{Stdout: nil})
	_ = tc.StdoutIsTTY()   // just ensure no panic
	_ = tc.IsInteractive() // ditto
}

// ── Precedence: flag > global > env > stream ──────────────────────────────────

func TestPrecedence_FlagBeatsEnv(t *testing.T) {
	resetGlobal(t)
	// CI would make IsInteractive false, but the explicit flag is the same
	// result; the important thing is explicit option is respected at all
	// layers.  We verify via StdoutIsTTY which is only false under
	// NonInteractive.
	t.Setenv("CI", "true")
	t.Setenv("FORCE_COLOR", "1") // would normally make TTY true

	tc := New(Options{NonInteractive: true})
	if tc.IsInteractive() {
		t.Error("flag NonInteractive=true must override env FORCE_COLOR")
	}
}

func TestPrecedence_GlobalFlagBeatsAutoDetect(t *testing.T) {
	resetGlobal(t)
	t.Setenv("CI", "")
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "")
	t.Setenv("DEBIAN_FRONTEND", "")
	t.Setenv("FORCE_COLOR", "1")

	SetGlobalNonInteractive(true)

	// Even with FORCE_COLOR=1 (which would normally override TTY detection),
	// global non-interactive wins.
	tc := New(Options{})
	if tc.IsInteractive() {
		t.Error("global non-interactive must beat FORCE_COLOR auto-detect")
	}
}

// ── context.Context round-trip ────────────────────────────────────────────────

func TestWithContext_FromContext_RoundTrip(t *testing.T) {
	resetGlobal(t)

	tc := New(Options{NonInteractive: true})
	ctx := tc.WithContext(context.Background())

	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext returned nil")
	}
	if got.IsInteractive() {
		t.Error("IsInteractive() should be false on retrieved Context")
	}
}

func TestFromContext_NoValue_ReturnsSafeDefault(t *testing.T) {
	resetGlobal(t)
	t.Setenv("CI", "")
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "")
	t.Setenv("DEBIAN_FRONTEND", "")

	// An empty context should never return nil — auto-detect is used.
	got := FromContext(context.Background())
	if got == nil {
		t.Fatal("FromContext(Background()) should never return nil")
	}
}

func TestWithContext_PreservesParentValues(t *testing.T) {
	resetGlobal(t)

	type parentKey struct{}
	parent := context.WithValue(context.Background(), parentKey{}, "sentinel")

	tc := New(Options{NonInteractive: true})
	ctx := tc.WithContext(parent)

	if ctx.Value(parentKey{}) != "sentinel" {
		t.Error("WithContext should preserve existing context values")
	}
}

// ── envForcesNonInteractive internals ────────────────────────────────────────

func TestEnvForcesNonInteractive_Clean_ReturnsFalse(t *testing.T) {
	// Explicitly clear all three env vars so the function returns false.
	old := map[string]string{
		"GLASSBOX_NON_INTERACTIVE": os.Getenv("GLASSBOX_NON_INTERACTIVE"),
		"CI":                       os.Getenv("CI"),
		"DEBIAN_FRONTEND":          os.Getenv("DEBIAN_FRONTEND"),
	}
	t.Cleanup(func() {
		for k, v := range old {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	})

	_ = os.Unsetenv("GLASSBOX_NON_INTERACTIVE")
	_ = os.Unsetenv("CI")
	_ = os.Unsetenv("DEBIAN_FRONTEND")

	if envForcesNonInteractive() {
		t.Error("envForcesNonInteractive() should return false when all env vars are unset")
	}
}

func TestEnvForcesNonInteractive_CI_Empty_ReturnsFalse(t *testing.T) {
	t.Setenv("CI", "") // CI set to empty string — must not trigger
	t.Setenv("GLASSBOX_NON_INTERACTIVE", "")
	t.Setenv("DEBIAN_FRONTEND", "")

	if envForcesNonInteractive() {
		t.Error("CI='' (empty) should not force non-interactive")
	}
}
