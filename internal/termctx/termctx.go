// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package termctx centralises TTY detection and non-interactive mode for the
// Glassbox CLI.
//
// # Why this package exists
//
// Before this package, every subsystem (visualizer, terminal/ansi, init wizard)
// independently queried os.Stdout.Fd() / os.Stdin.Fd() and checked environment
// variables. This meant:
//   - No single place to apply --non-interactive
//   - Tests had no clean seam to inject a pipe-backed stream
//   - CI detection (piped stdout) was duplicated and inconsistent
//
// # Precedence rules (highest → lowest)
//
//  1. Explicit flag: --non-interactive forces non-interactive mode regardless of
//     anything else, including FORCE_COLOR.
//  2. Environment:   CI=true, DEBIAN_FRONTEND=noninteractive, or
//     GLASSBOX_NON_INTERACTIVE=1 also forces non-interactive mode.
//  3. Stream probe:  if the supplied stdout is not a real TTY the session is
//     non-interactive (pipe, redirect, test writer).
//  4. Auto-detect:   fall back to os.Stdout.Fd() via go-isatty when no
//     override is in effect.
//
// # Color vs interactivity
//
// "Interactive" and "colored" are related but distinct. Non-interactive mode
// suppresses prompts, spinners, and cursor-control sequences. It does NOT
// automatically disable color — that is controlled by NO_COLOR / FORCE_COLOR /
// --no-color as before. Non-interactive output contains no control sequences
// beyond those used for color (i.e. no cursor movement, no ClearLine).
//
// # Usage
//
//	ctx := termctx.New(termctx.Options{NonInteractive: nonInteractiveFlag})
//	if ctx.IsInteractive() {
//	    spinner.Start(...)
//	    prompt(...)
//	}
//	if ctx.StdoutIsTTY() {
//	    // color is safe
//	}
package termctx

import (
	"context"
	"io"
	"os"
	"sync/atomic"

	"github.com/mattn/go-isatty"
)

// contextKey is the unexported key used to store a *Context in a context.Context.
type contextKey struct{}

// Options configure a terminal Context. Zero value is safe: auto-detection is
// used for all fields.
type Options struct {
	// NonInteractive forces non-interactive mode when true. Maps to the
	// --non-interactive CLI flag. Takes precedence over all other settings.
	NonInteractive bool

	// Stdout is the writer whose file descriptor is probed for TTY status.
	// When nil, os.Stdout is used.
	Stdout io.Writer

	// Stdin is the reader whose file descriptor is probed for interactive
	// prompt capability. When nil, os.Stdin is used.
	Stdin io.Reader
}

// Context carries terminal capability information for a single command run.
// It is safe for concurrent use after construction.
type Context struct {
	nonInteractive bool // resolved once at construction
	stdoutIsTTY    bool // resolved once at construction
	stdinIsTTY     bool // resolved once at construction
}

// globalNonInteractive allows the root PersistentPreRunE to activate
// non-interactive mode globally (e.g. when --non-interactive is set as a
// persistent flag) before per-command Contexts are created.
var globalNonInteractive atomic.Bool

// SetGlobalNonInteractive activates non-interactive mode process-wide.
// It is called once from rootCmd.PersistentPreRunE when --non-interactive is
// set. Subsequent calls to New() will inherit this setting.
func SetGlobalNonInteractive(v bool) {
	globalNonInteractive.Store(v)
}

// GlobalNonInteractive reports the process-wide non-interactive override.
func GlobalNonInteractive() bool {
	return globalNonInteractive.Load()
}

// New constructs a Context using the provided options plus environment probing.
// It is inexpensive and safe to call at the start of every command RunE.
func New(opts Options) *Context {
	c := &Context{}

	// Resolve non-interactive flag: explicit opt > global flag > env detection.
	if opts.NonInteractive || GlobalNonInteractive() || envForcesNonInteractive() {
		c.nonInteractive = true
		// When forced non-interactive, TTY probes are irrelevant — treat both
		// as false so callers don't need to check both fields.
		c.stdoutIsTTY = false
		c.stdinIsTTY = false
		return c
	}

	// Probe stdout.
	if f, ok := resolveFile(opts.Stdout, os.Stdout); ok {
		c.stdoutIsTTY = isatty.IsTerminal(f.Fd())
	}

	// Probe stdin.
	if f, ok := resolveFile(opts.Stdin, os.Stdin); ok {
		c.stdinIsTTY = isatty.IsTerminal(f.Fd())
	}

	// If stdout is not a TTY the session is non-interactive even without an
	// explicit flag (pipe, redirect, CI runner with captured output).
	if !c.stdoutIsTTY {
		c.nonInteractive = true
	}

	return c
}

// IsInteractive reports whether the current session supports prompts, spinners,
// and other interactive terminal features.
//
// Returns false when:
//   - --non-interactive was set
//   - CI / GLASSBOX_NON_INTERACTIVE env vars are set
//   - stdout is not a TTY (pipe or redirect)
func (c *Context) IsInteractive() bool {
	return !c.nonInteractive
}

// StdoutIsTTY reports whether stdout is connected to a real terminal.
// Color output is safe when this returns true (subject to NO_COLOR etc).
func (c *Context) StdoutIsTTY() bool {
	return c.stdoutIsTTY
}

// StdinIsTTY reports whether stdin is connected to a real terminal.
// Use this to decide whether reading a line from stdin will block for user input.
func (c *Context) StdinIsTTY() bool {
	return c.stdinIsTTY
}

// WithContext stores this terminal Context into a context.Context so it can be
// retrieved by sub-functions without threading it through every call site.
func (c *Context) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// FromContext retrieves the terminal Context stored by WithContext. If none was
// stored it returns a safe auto-detected Context so callers never need to nil-check.
func FromContext(ctx context.Context) *Context {
	if c, ok := ctx.Value(contextKey{}).(*Context); ok && c != nil {
		return c
	}
	return New(Options{})
}

// envForcesNonInteractive returns true when well-known CI / non-interactive
// environment variables are set.
func envForcesNonInteractive() bool {
	// GLASSBOX_NON_INTERACTIVE=1 is our own escape hatch.
	if os.Getenv("GLASSBOX_NON_INTERACTIVE") == "1" {
		return true
	}
	// CI=true is set by virtually every CI system (GitHub Actions, CircleCI,
	// Travis CI, Jenkins via plugin, etc.).
	if os.Getenv("CI") != "" {
		return true
	}
	// DEBIAN_FRONTEND=noninteractive is common in containerised environments.
	if os.Getenv("DEBIAN_FRONTEND") == "noninteractive" {
		return true
	}
	return false
}

// resolveFile extracts an *os.File from w (which may be an io.Writer or io.Reader).
// Falls back to fallback when w is nil. Returns (nil, false) when the value is
// not an *os.File and therefore cannot be probed for TTY status.
func resolveFile(w interface{}, fallback *os.File) (*os.File, bool) {
	if w == nil {
		return fallback, fallback != nil
	}
	if f, ok := w.(*os.File); ok {
		return f, true
	}
	return nil, false
}
