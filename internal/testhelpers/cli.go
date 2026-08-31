// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package testhelpers

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

// CLIFixture wraps a CLI invocation used in integration regression tests.
// It holds the binary path, arguments, environment overrides, and captured
// stdout/stderr.
type CLIFixture struct {
	BinaryPath string
	Args       []string
	Env        map[string]string
	Stdin      string
	ExpectFail bool
}

// NewCLIFixture creates a new CLI fixture with default settings.
func NewCLIFixture(binaryPath string, args ...string) *CLIFixture {
	return &CLIFixture{
		BinaryPath: binaryPath,
		Args:       args,
		Env:        make(map[string]string),
	}
}

// WithEnv sets an environment variable for the invocation.
func (f *CLIFixture) WithEnv(key, value string) *CLIFixture {
	f.Env[key] = value
	return f
}

// WithStdin sets the stdin input for the command.
func (f *CLIFixture) WithStdin(s string) *CLIFixture {
	f.Stdin = s
	return f
}

// ExpectFailure marks the command as expected to fail.
// Use this to test error-path validation without failing the test.
func (f *CLIFixture) ExpectFailure() *CLIFixture {
	f.ExpectFail = true
	return f
}

// Run executes the CLI command and returns stdout, stderr, and exit error.
// It does not fail the test on command error; callers must assert on the
// returned values explicitly.
func (f *CLIFixture) Run(t *testing.T, ctx context.Context) (stdout, stderr string, err error) {
	t.Helper()

	cmd := exec.CommandContext(ctx, f.BinaryPath, f.Args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if f.Stdin != "" {
		cmd.Stdin = strings.NewReader(f.Stdin)
	}

	if len(f.Env) > 0 {
		// Merge overrides with the current environment.
		cmd.Env = append(cmd.Environ(), envMapToSlice(f.Env)...)
	}
	// Note: exec.Cmd.Environ() returns the process env merged with cmd.Env.
	// When Env is nil (unset) it returns os.Environ(). This is the correct
	// merge semantics for inheriting the parent environment with overrides.

	err = cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if !f.ExpectFail && err != nil {
		t.Logf("[CLIFixture] Unexpected command failure: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr)
	}

	return stdout, stderr, err
}

// AssertContains asserts that the combined output (stdout + stderr) contains
// the expected substring. It fails the test if the substring is missing.
func (f *CLIFixture) AssertContains(t *testing.T, ctx context.Context, expected string) {
	t.Helper()
	stdout, stderr, _ := f.Run(t, ctx)
	combined := stdout + stderr
	if !strings.Contains(combined, expected) {
		t.Errorf("CLI output missing expected substring:\nExpected: %q\nGot:\n%s",
			expected, combined)
	}
}

// AssertNotContains asserts that the combined output does NOT contain the
// given substring.
func (f *CLIFixture) AssertNotContains(t *testing.T, ctx context.Context, unwanted string) {
	t.Helper()
	stdout, stderr, _ := f.Run(t, ctx)
	combined := stdout + stderr
	if strings.Contains(combined, unwanted) {
		t.Errorf("CLI output unexpectedly contained substring:\nUnwanted: %q\nGot:\n%s",
			unwanted, combined)
	}
}

// envMapToSlice converts a map of env overrides into shell KEY=VALUE strings.
func envMapToSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
