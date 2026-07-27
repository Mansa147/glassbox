// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package sourcemap

import "fmt"

// MatchKind classifies how a WASM address was mapped to source.
type MatchKind string

const (
	// MatchExactOffset — DWARF line table covers the address with file and line.
	MatchExactOffset MatchKind = "exact_offset"
	// MatchFunction — subprogram bounds cover the address; line may be approximate.
	MatchFunction MatchKind = "function"
	// MatchLineTable — file inferred from partial .debug_line when .debug_info is stripped.
	MatchLineTable MatchKind = "line_table"
	// MatchHeuristic — symbol name or Cargo manifest inference.
	MatchHeuristic MatchKind = "heuristic"
	// MatchUnknown — no mapping could be produced.
	MatchUnknown MatchKind = "unknown"
)

// LinkPresentation describes whether a GitHub (or registry) link is emitted automatically.
type LinkPresentation string

const (
	// LinkAuto — confidence meets AutoLinkMinConfidence; link is authoritative.
	LinkAuto LinkPresentation = "auto"
	// LinkCandidate — lower confidence; link is shown as a suggestion only.
	LinkCandidate LinkPresentation = "candidate"
	// LinkNone — confidence too low or location empty; no link is generated.
	LinkNone LinkPresentation = "none"
)

const (
	// ConfidenceExactOffset is the score for a DWARF line-table hit at the address.
	ConfidenceExactOffset = 100
	// ConfidenceFunction is the score for a subprogram-only match.
	ConfidenceFunction = 72
	// ConfidenceLineTable is the score for partial DWARF file inference.
	ConfidenceLineTable = 48
	// ConfidenceHeuristicSymbol is the score for symbol-name inference.
	ConfidenceHeuristicSymbol = 28
	// ConfidenceHeuristicCargo is the score for Cargo.toml discovery inference.
	ConfidenceHeuristicCargo = 22
	// ConfidenceUnknown is the score when no location is resolved.
	ConfidenceUnknown = 0

	// AutoLinkMinConfidence is the minimum score to auto-generate a source link.
	AutoLinkMinConfidence = 60
	// CandidateMinConfidence is the minimum score to present a link as a candidate.
	CandidateMinConfidence = 15
)

// MappingEvidence retains debugging fields for scoring decisions. Raw file/line
// values remain on FallbackResult so consumers can filter by confidence without
// losing locations.
type MappingEvidence struct {
	MatchKind   MatchKind `json:"match_kind"`
	Stage       string    `json:"stage,omitempty"`
	WasmAddr    uint64    `json:"wasm_addr,omitempty"`
	Function    string    `json:"function,omitempty"`
	Package     string    `json:"package,omitempty"`
	LineKnown   bool      `json:"line_known"`
	ColumnKnown bool      `json:"column_known"`
	Heuristic   bool      `json:"heuristic,omitempty"`
}

// ConfidenceForKind returns a stable score for the given match kind and context.
// Scores depend only on mapping inputs, not wall time or environment.
func ConfidenceForKind(kind MatchKind, lineKnown, columnKnown bool, heuristicStage string) int {
	switch kind {
	case MatchExactOffset:
		if !lineKnown {
			return ConfidenceFunction
		}
		if columnKnown {
			return ConfidenceExactOffset
		}
		return ConfidenceExactOffset - 2
	case MatchFunction:
		return ConfidenceFunction
	case MatchLineTable:
		return ConfidenceLineTable
	case MatchHeuristic:
		if heuristicStage == "cargo_manifest" {
			return ConfidenceHeuristicCargo
		}
		return ConfidenceHeuristicSymbol
	default:
		return ConfidenceUnknown
	}
}

// LinkPresentationForConfidence maps a score to auto/candidate/none link policy.
func LinkPresentationForConfidence(score int, hasFile bool) LinkPresentation {
	if !hasFile || score <= ConfidenceUnknown {
		return LinkNone
	}
	if score >= AutoLinkMinConfidence {
		return LinkAuto
	}
	if score >= CandidateMinConfidence {
		return LinkCandidate
	}
	return LinkNone
}

func applyConfidence(result *FallbackResult, addr uint64, kind MatchKind, stage string, pkg string) {
	if result == nil {
		return
	}
	lineKnown := result.Line > 0
	columnKnown := result.Column > 0
	result.MatchKind = kind
	result.Evidence = MappingEvidence{
		MatchKind:   kind,
		Stage:       stage,
		WasmAddr:    addr,
		Function:    result.Function,
		Package:     pkg,
		LineKnown:   lineKnown,
		ColumnKnown: columnKnown,
		Heuristic:   kind == MatchHeuristic,
	}
	result.Confidence = ConfidenceForKind(kind, lineKnown, columnKnown, stage)
	result.LinkPresentation = LinkPresentationForConfidence(result.Confidence, result.File != "")
}

// UserSummary formats a concise, user-visible mapping description including confidence.
func (r *FallbackResult) UserSummary() string {
	if r == nil {
		return ""
	}
	kind := r.MatchKind
	if kind == "" {
		kind = MatchUnknown
	}
	switch {
	case r.File != "" && r.Line > 0:
		return fmt.Sprintf("%s:%d (confidence %d, %s)", r.File, r.Line, r.Confidence, kind)
	case r.File != "":
		label := string(kind)
		if r.Evidence.Heuristic {
			label = "heuristic"
		}
		return fmt.Sprintf("%s (confidence %d, %s — line unknown)", r.File, r.Confidence, label)
	case r.Quality == MappingQualityUnknown:
		return fmt.Sprintf("unmapped (confidence %d)", r.Confidence)
	default:
		return fmt.Sprintf("confidence %d, %s", r.Confidence, kind)
	}
}
