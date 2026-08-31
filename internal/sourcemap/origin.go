// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package sourcemap — origin classification for DWARF-resolved source paths.
//
// OriginClass tags every resolved source location so trace viewers and
// formatters can present user-authored code differently from generated build
// artifacts and external crate dependencies without hiding any frames.
//
// Classification is heuristic-based and opt-in: callers pass the resolved
// path to Classifier.Classify and receive one of four OriginClass values.
// The classifier never touches the filesystem; it works purely on path
// strings so it is cheap to call for every frame in a trace.

package sourcemap

import (
	"path/filepath"
	"strings"
)

// OriginClass describes where a resolved source path came from.
// The zero value is OriginUnknown.
//
// The string representation of each constant matches the JSON value used in
// trace exports (see internal/trace/splitpane.go's SourceRef.OriginClass).
// Keep these values stable — they are part of the published JSON schema.
type OriginClass string

const (
	// OriginUser is user-authored source code under the project root.
	// Frames with this class are shown without additional decoration.
	OriginUser OriginClass = "user"

	// OriginGenerated is machine-generated build output: Rust WASM artifacts
	// under target/, macro-expanded code, proc-macro output, or other files
	// produced during compilation rather than hand-written by the developer.
	// Trace formatters label these frames "[generated]" so developers know
	// the path may not exist in their workspace.
	OriginGenerated OriginClass = "generated"

	// OriginExternal is source code from an external crate or a dependency
	// outside the project workspace.  This includes Cargo registry paths,
	// Git checkouts, and any absolute path that falls outside the project
	// root.  Formatters label these frames "[external]".
	OriginExternal OriginClass = "external"

	// OriginUnknown is used when the classifier cannot determine the origin,
	// typically because the path is empty or does not match any known pattern.
	OriginUnknown OriginClass = "unknown"
)

// ClassifierOptions configures the heuristics applied by Classifier.Classify.
// All fields are optional; zero values produce reasonable defaults.
type ClassifierOptions struct {
	// ProjectRoot is the absolute path to the workspace root.  When set, any
	// absolute path that is not a child of ProjectRoot and not under a known
	// build directory is classified as OriginExternal.
	ProjectRoot string

	// ExtraBuildDirs is an optional list of additional build-directory
	// prefixes to treat as OriginGenerated (relative or absolute).
	// These are checked after the built-in patterns.
	ExtraBuildDirs []string

	// ExtraExternalPrefixes is an optional list of path prefixes (relative or
	// absolute) that should always be classified as OriginExternal.
	ExtraExternalPrefixes []string
}

// Classifier classifies DWARF-resolved source paths by their origin.
// Construct with NewClassifier and reuse across multiple Classify calls.
type Classifier struct {
	opts ClassifierOptions
}

// NewClassifier creates a Classifier configured with the given options.
// Passing a zero-value ClassifierOptions is valid and produces a classifier
// that applies only the built-in heuristics.
func NewClassifier(opts ClassifierOptions) *Classifier {
	// Normalise the project root once at construction time.
	if opts.ProjectRoot != "" {
		opts.ProjectRoot = filepath.ToSlash(opts.ProjectRoot)
		opts.ProjectRoot = strings.TrimRight(opts.ProjectRoot, "/")
	}
	return &Classifier{opts: opts}
}

// Classify returns the OriginClass for the given raw source path.
//
// Classification rules (applied in priority order):
//
//  1. Empty path → OriginUnknown.
//  2. Path is under a Cargo registry or Cargo git checkout → OriginExternal.
//  3. Path ends with .wasm or contains target/wasm32 → OriginGenerated.
//  4. Path contains /target/ (or starts with target/) → OriginGenerated.
//  5. ExtraBuildDirs match → OriginGenerated.
//  6. ExtraExternalPrefixes match → OriginExternal.
//  7. ProjectRoot is set and path is absolute and not under ProjectRoot
//     → OriginExternal.
//  8. Otherwise → OriginUser.
//
// The rules are intentionally conservative: a path that matches neither a
// build-directory pattern nor an external-dependency pattern is classified
// as OriginUser so user frames are never accidentally hidden.
func (c *Classifier) Classify(rawPath string) OriginClass {
	if rawPath == "" {
		return OriginUnknown
	}

	// Normalise separators for consistent matching.
	p := filepath.ToSlash(rawPath)

	// ── 1. Cargo registry / git dependency paths → external ────────────────
	if isCargoPath(p) {
		return OriginExternal
	}

	// ── 2. WASM artifacts and wasm32 target directory → generated ───────────
	if strings.HasSuffix(p, ".wasm") ||
		strings.Contains(p, "target/wasm32-unknown-unknown") ||
		strings.Contains(p, "target/wasm32") {
		return OriginGenerated
	}

	// ── 3. Generic Rust build output directories → generated ────────────────
	if strings.Contains(p, "/target/") || strings.HasPrefix(p, "target/") {
		return OriginGenerated
	}

	// ── 4. Caller-supplied extra build directories → generated ───────────────
	for _, dir := range c.opts.ExtraBuildDirs {
		normalised := filepath.ToSlash(strings.TrimRight(dir, "/"))
		if normalised != "" && (strings.Contains(p, normalised) || strings.HasPrefix(p, normalised)) {
			return OriginGenerated
		}
	}

	// ── 5. Caller-supplied extra external prefixes → external ───────────────
	for _, prefix := range c.opts.ExtraExternalPrefixes {
		normalised := filepath.ToSlash(strings.TrimRight(prefix, "/"))
		if normalised != "" && strings.HasPrefix(p, normalised) {
			return OriginExternal
		}
	}

	// ── 6. Absolute path outside the project root → external ─────────────────
	if c.opts.ProjectRoot != "" && isAbsoluteSlash(p) {
		if !strings.HasPrefix(p, c.opts.ProjectRoot+"/") && p != c.opts.ProjectRoot {
			return OriginExternal
		}
	}

	// ── 7. Default → user source ─────────────────────────────────────────────
	return OriginUser
}

// ClassifyPath is a convenience wrapper that creates a one-shot Classifier
// with only the ProjectRoot configured and classifies the given path.
// For hot paths (e.g. classifying many frames in a single trace), prefer
// constructing a Classifier once and calling Classify repeatedly.
func ClassifyPath(rawPath, projectRoot string) OriginClass {
	return NewClassifier(ClassifierOptions{ProjectRoot: projectRoot}).Classify(rawPath)
}

// Label returns a short human-readable label for the origin class suitable for
// appending to a source location line in terminal and JSON output.
//
//	OriginUser      → ""           (no annotation; user code is the happy path)
//	OriginGenerated → "[generated]"
//	OriginExternal  → "[external]"
//	OriginUnknown   → "[unknown origin]"
func (c OriginClass) Label() string {
	switch c {
	case OriginGenerated:
		return "[generated]"
	case OriginExternal:
		return "[external]"
	case OriginUnknown:
		return "[unknown origin]"
	default:
		return ""
	}
}

// ── Internal helpers ───────────────────────────────────────────────────────

// isCargoPath returns true for paths that come from the Cargo registry or a
// Cargo git checkout — these are always external dependencies.
func isCargoPath(p string) bool {
	return strings.Contains(p, "/.cargo/registry") ||
		strings.Contains(p, "/.cargo/git") ||
		strings.Contains(p, ".cargo/registry/src/") ||
		strings.Contains(p, "/registry/src/") ||
		strings.Contains(p, "\\.cargo\\registry") ||
		strings.Contains(p, "\\.cargo\\git")
}

// isAbsoluteSlash reports whether the forward-slash normalised path is absolute
// (starts with / or has a Windows drive letter prefix like C:/).
func isAbsoluteSlash(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	// Windows drive letter: C:/...
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		return true
	}
	return false
}
