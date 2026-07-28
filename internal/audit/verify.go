// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirVerifyResult is the outcome of verifying an audit log directory.
type DirVerifyResult struct {
	Valid            bool     `json:"valid"`
	SegmentsChecked  int      `json:"segments_checked"`
	ChainValid       bool     `json:"chain_valid"`
	ActivePresent    bool     `json:"active_present"`
	Issues           []string `json:"issues,omitempty"`
	FirstSegmentHash string   `json:"first_segment_hash,omitempty"`
	LastSegmentHash  string   `json:"last_segment_hash,omitempty"`
}

// VerifyDirectory verifies closed segment manifests and checksum chaining in
// dir. It reuses the same SHA-256 hex validation primitive as audit:verify
// chain links and checks that each manifest's previous_segment_hash matches
// the prior segment body hash.
func VerifyDirectory(dir string) (*DirVerifyResult, error) {
	result := &DirVerifyResult{ChainValid: true}

	if st, err := os.Stat(filepath.Join(dir, ActiveSegmentName)); err == nil && !st.IsDir() {
		result.ActivePresent = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("audit: read log directory %q: %w", dir, err)
	}

	// Detect segment bodies missing manifests (interrupted rotation).
	type namedSeg struct {
		name string
		seq  uint64
	}
	var bodies []namedSeg
	manifests := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ManifestSuffix) {
			manifests[name] = struct{}{}
			continue
		}
		if seq, ok := ParseSegmentSequence(name); ok {
			bodies = append(bodies, namedSeg{name: name, seq: seq})
		}
	}

	for _, b := range bodies {
		manName := strings.TrimSuffix(b.name, ".jsonl") + ManifestSuffix
		if _, ok := manifests[manName]; !ok {
			result.Valid = false
			result.ChainValid = false
			result.Issues = append(result.Issues, fmt.Sprintf(
				"segment %q is missing its immutable manifest (possible interrupted rotation)", b.name))
		}
	}

	// Detect manifests whose segment body is missing.
	for manName := range manifests {
		segName := strings.TrimSuffix(manName, ManifestSuffix) + ".jsonl"
		found := false
		for _, b := range bodies {
			if b.name == segName {
				found = true
				break
			}
		}
		if !found {
			result.Valid = false
			result.ChainValid = false
			result.Issues = append(result.Issues, fmt.Sprintf(
				"manifest %q references missing segment %q", manName, segName))
		}
	}

	segments, listErr := ListSegments(dir)
	if listErr != nil {
		// Still return structural issues collected above.
		result.Valid = false
		result.ChainValid = false
		result.Issues = append(result.Issues, listErr.Error())
		return result, nil
	}

	var prevHash string
	var expectSeq uint64 = 1
	for i, s := range segments {
		result.SegmentsChecked++

		if s.Manifest.SchemaVersion != "" && s.Manifest.SchemaVersion != SchemaVersion {
			result.Valid = false
			result.Issues = append(result.Issues, fmt.Sprintf(
				"segment %q has unsupported schema_version %q", s.Manifest.Segment, s.Manifest.SchemaVersion))
		}

		if s.Manifest.Sequence != expectSeq {
			result.Valid = false
			result.ChainValid = false
			result.Issues = append(result.Issues, fmt.Sprintf(
				"missing segment in chain: expected sequence %d, found %d (%s)",
				expectSeq, s.Manifest.Sequence, s.Manifest.Segment))
			// Continue verifying what remains, advancing expectation to found seq+1.
			expectSeq = s.Manifest.Sequence
		}
		expectSeq++

		if err := ValidateSHA256Hex("sha256", s.Manifest.SHA256); err != nil {
			result.Valid = false
			result.ChainValid = false
			result.Issues = append(result.Issues, fmt.Sprintf("segment %q: %v", s.Manifest.Segment, err))
			continue
		}

		actual, err := HashFile(s.Path)
		if err != nil {
			result.Valid = false
			result.ChainValid = false
			result.Issues = append(result.Issues, err.Error())
			continue
		}
		if !strings.EqualFold(actual, s.Manifest.SHA256) {
			result.Valid = false
			result.ChainValid = false
			result.Issues = append(result.Issues, fmt.Sprintf(
				"segment %q checksum mismatch: manifest=%s file=%s",
				s.Manifest.Segment, shortHash(s.Manifest.SHA256), shortHash(actual)))
		}

		if i == 0 {
			if s.Manifest.PreviousSegmentHash != "" {
				result.Valid = false
				result.ChainValid = false
				result.Issues = append(result.Issues, fmt.Sprintf(
					"genesis segment %q must not set previous_segment_hash", s.Manifest.Segment))
			}
			result.FirstSegmentHash = actual
		} else {
			if err := ValidateSHA256Hex("previous_segment_hash", s.Manifest.PreviousSegmentHash); err != nil {
				result.Valid = false
				result.ChainValid = false
				result.Issues = append(result.Issues, fmt.Sprintf("segment %q: %v", s.Manifest.Segment, err))
			} else if !strings.EqualFold(s.Manifest.PreviousSegmentHash, prevHash) {
				result.Valid = false
				result.ChainValid = false
				result.Issues = append(result.Issues, fmt.Sprintf(
					"chain link broken at %q: previous_segment_hash %s does not match predecessor %s",
					s.Manifest.Segment, shortHash(s.Manifest.PreviousSegmentHash), shortHash(prevHash)))
			}
		}

		prevHash = actual
		result.LastSegmentHash = actual
	}

	if len(result.Issues) == 0 {
		result.Valid = true
		result.ChainValid = true
	} else {
		result.Valid = false
	}

	return result, nil
}

func shortHash(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if len(h) <= 12 {
		return h
	}
	return h[:8] + "…" + h[len(h)-4:]
}
