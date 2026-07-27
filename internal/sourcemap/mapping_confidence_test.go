// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package sourcemap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfidenceForKind_Ordering(t *testing.T) {
	exact := ConfidenceForKind(MatchExactOffset, true, true, "")
	function := ConfidenceForKind(MatchFunction, true, false, "")
	lineTable := ConfidenceForKind(MatchLineTable, false, false, "")
	heuristic := ConfidenceForKind(MatchHeuristic, false, false, "symbol_heuristic")
	unknown := ConfidenceForKind(MatchUnknown, false, false, "")

	assert.Greater(t, exact, function)
	assert.Greater(t, function, lineTable)
	assert.Greater(t, lineTable, heuristic)
	assert.Greater(t, heuristic, unknown)
	assert.Equal(t, ConfidenceExactOffset, exact)
	assert.Equal(t, ConfidenceHeuristicCargo, ConfidenceForKind(MatchHeuristic, false, false, "cargo_manifest"))
}

func TestConfidenceForKind_StableAcrossCalls(t *testing.T) {
	for i := 0; i < 5; i++ {
		assert.Equal(t, 98, ConfidenceForKind(MatchExactOffset, true, false, ""))
		assert.Equal(t, ConfidenceLineTable, ConfidenceForKind(MatchLineTable, false, false, ""))
	}
}

func TestLinkPresentationForConfidence_Thresholds(t *testing.T) {
	assert.Equal(t, LinkAuto, LinkPresentationForConfidence(ConfidenceExactOffset, true))
	assert.Equal(t, LinkAuto, LinkPresentationForConfidence(AutoLinkMinConfidence, true))
	assert.Equal(t, LinkCandidate, LinkPresentationForConfidence(CandidateMinConfidence, true))
	assert.Equal(t, LinkCandidate, LinkPresentationForConfidence(ConfidenceHeuristicSymbol, true))
	assert.Equal(t, LinkNone, LinkPresentationForConfidence(ConfidenceUnknown, true))
	assert.Equal(t, LinkNone, LinkPresentationForConfidence(ConfidenceExactOffset, false))
}

func TestFallbackResult_UserSummary_HeuristicLabeled(t *testing.T) {
	r := &FallbackResult{
		File:       "/project/src/lib.rs",
		Confidence: ConfidenceHeuristicSymbol,
		MatchKind:  MatchHeuristic,
		Evidence:   MappingEvidence{Heuristic: true},
		Quality:    MappingQualityHeuristic,
	}
	s := r.UserSummary()
	assert.Contains(t, s, "heuristic")
	assert.Contains(t, s, "confidence 28")
}

func TestApplyConfidence_PreservesRawLocation(t *testing.T) {
	r := &FallbackResult{
		File:   "src/token.rs",
		Line:   99,
		Column: 4,
	}
	applyConfidence(r, 0x200, MatchExactOffset, "full_dwarf", "")
	assert.Equal(t, "src/token.rs", r.File)
	assert.Equal(t, 99, r.Line)
	assert.Equal(t, 4, r.Column)
	assert.Equal(t, MatchExactOffset, r.MatchKind)
	assert.Equal(t, LinkAuto, r.LinkPresentation)
}
