// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ViewerStateVersion is the current on-disk schema version for viewer state
// sidecar files. Files written with a newer version than this are ignored
// (with a warning) rather than misinterpreted.
const ViewerStateVersion = 1

// ViewerStateDirEnv, when set, overrides the directory used to store viewer
// state sidecar files. Useful when the home directory is read-only.
const ViewerStateDirEnv = "GLASSBOX_VIEWER_STATE_DIR"

const (
	maxSearchQueryLen = 512
	maxEventFilterLen = 64
	maxTxHashLen      = 128
)

var (
	stateMu sync.Mutex
	// writesDisabled is set after the first failed sidecar write so that a
	// read-only state directory produces one warning, not one per keystroke.
	writesDisabled bool
	// warnWriter receives sidecar warnings; swapped out by tests.
	warnWriter io.Writer = os.Stderr
)

// ViewerState holds the UI fields persisted per trace. It is stored as a
// standalone JSON sidecar keyed by trace fingerprint, never inside the trace
// payload itself. Only plain display state is stored — no paths, commands, or
// other executable content.
type ViewerState struct {
	Version          int       `json:"version"`
	TraceFingerprint string    `json:"trace_fingerprint"`
	TxHash           string    `json:"tx_hash,omitempty"`
	CurrentStep      int       `json:"current_step"`
	SearchQuery      string    `json:"search_query,omitempty"`
	CurrentMatch     int       `json:"current_match,omitempty"` // 1-based
	EventFilter      string    `json:"event_filter,omitempty"`
	HideStdLib       bool      `json:"hide_stdlib,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

// viewerStateDir resolves the sidecar directory without creating it.
func viewerStateDir() (string, error) {
	if dir := os.Getenv(ViewerStateDirEnv); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".Glassbox", "viewer_state"), nil
}

// fingerprintOK reports whether fp is a plausible hex fingerprint that is
// safe to embed in a file name (no separators, no traversal).
func fingerprintOK(fp string) bool {
	if len(fp) < 16 || len(fp) > 128 {
		return false
	}
	for _, r := range fp {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func sidecarPath(fp string) (string, error) {
	if !fingerprintOK(fp) {
		return "", fmt.Errorf("invalid trace fingerprint %q", fp)
	}
	dir, err := viewerStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, strings.ToLower(fp)+".json"), nil
}

// sanitizeText strips control characters and truncates to max runes, so that
// persisted state can never smuggle escape sequences or oversized blobs back
// into the terminal.
func sanitizeText(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// sanitize normalizes a ViewerState so that loaded or saved values are always
// bounded, printable, and non-negative.
func (st *ViewerState) sanitize() {
	st.SearchQuery = sanitizeText(st.SearchQuery, maxSearchQueryLen)
	st.EventFilter = sanitizeText(st.EventFilter, maxEventFilterLen)
	st.TxHash = sanitizeText(st.TxHash, maxTxHashLen)
	if st.CurrentStep < 0 {
		st.CurrentStep = 0
	}
	if st.CurrentMatch < 0 {
		st.CurrentMatch = 0
	}
}

func warnf(format string, args ...interface{}) {
	fmt.Fprintf(warnWriter, "glassbox: warning: "+format+"\n", args...)
}

// LoadViewerState returns the persisted state for the trace identified by
// fingerprint, if present and valid. A missing, corrupted, stale, or
// incompatible sidecar is never an error: corruption and version mismatches
// are reported with a warning and the sidecar is ignored, so the viewer
// always starts from defaults rather than failing or applying bad state.
func LoadViewerState(fingerprint string) (ViewerState, bool, error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	path, err := sidecarPath(fingerprint)
	if err != nil {
		return ViewerState{}, false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ViewerState{}, false, nil
		}
		warnf("cannot read viewer state sidecar %s: %v — ignoring", path, err)
		return ViewerState{}, false, nil
	}

	var st ViewerState
	if err := json.Unmarshal(b, &st); err != nil {
		warnf("viewer state sidecar %s is corrupted: %v — ignoring; it will be overwritten on exit", path, err)
		return ViewerState{}, false, nil
	}
	if st.Version != ViewerStateVersion {
		warnf("viewer state sidecar %s has unsupported version %d (this build supports %d) — ignoring", path, st.Version, ViewerStateVersion)
		return ViewerState{}, false, nil
	}
	if !strings.EqualFold(st.TraceFingerprint, fingerprint) {
		// The trace content this state was saved for no longer matches the
		// trace being opened; applying it could select the wrong step.
		warnf("viewer state sidecar %s was saved for a different trace — ignoring stale state", path)
		return ViewerState{}, false, nil
	}

	st.sanitize()
	return st, true, nil
}

// SaveViewerState atomically persists st as the sidecar for fingerprint. The
// record is written to a temporary file in the same directory and renamed
// into place, so a crash mid-write can never leave a truncated sidecar.
//
// If the state directory is not writable (e.g. a read-only home), persistence
// is disabled for the remainder of the process after a single warning; the
// viewer keeps working without it.
func SaveViewerState(fingerprint string, st ViewerState) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	if writesDisabled {
		return nil
	}
	path, err := sidecarPath(fingerprint)
	if err != nil {
		return err
	}

	st.Version = ViewerStateVersion
	st.TraceFingerprint = strings.ToLower(fingerprint)
	st.UpdatedAt = time.Now().UTC()
	st.sanitize()

	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal viewer state: %w", err)
	}

	if err := writeSidecarAtomic(path, out); err != nil {
		writesDisabled = true
		warnf("cannot write viewer state sidecar %s: %v — viewer state persistence disabled for this session", path, err)
		return err
	}
	return nil
}

func writeSidecarAtomic(path string, data []byte) error {
	return writeFileAtomic(path, data, 0o600)
}

// ResetViewerState deletes the persisted sidecar for fingerprint. It reports
// whether a sidecar existed.
func ResetViewerState(fingerprint string) (bool, error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	path, err := sidecarPath(fingerprint)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ResetAllViewerState deletes every viewer state sidecar and returns how many
// were removed.
func ResetAllViewerState() (int, error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	dir, err := viewerStateDir()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	var firstErr error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
