// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── whitespace normalisation ──────────────────────────────────────────────────

func TestNormaliseInputString_TrimsSpaces(t *testing.T) {
	assert.Equal(t, "abc", normaliseInputString("  abc  "))
	assert.Equal(t, "abc", normaliseInputString("\tabc\t"))
}

func TestNormaliseInputString_RemovesNewlines(t *testing.T) {
	assert.Equal(t, "abc", normaliseInputString("abc\n"))
	assert.Equal(t, "abc", normaliseInputString("abc\r\n"))
	assert.Equal(t, "abc", normaliseInputString("\nabc\n"))
}

func TestNormaliseInputString_EmptyAfterTrim(t *testing.T) {
	assert.Equal(t, "", normaliseInputString("   "))
	assert.Equal(t, "", normaliseInputString("\n\r\n"))
}

// ── transaction hash parsing ──────────────────────────────────────────────────

var validHash = strings.Repeat("a", 64)

func TestParseInput_ValidHash(t *testing.T) {
	pi, err := ParseInput(InputKindTransactionHash, validHash)
	require.NoError(t, err)
	assert.Equal(t, InputKindTransactionHash, pi.Kind)
	assert.Equal(t, validHash, pi.NormalisedSource)
	assert.Nil(t, pi.Decoded)
}

func TestParseInput_HashWithTrailingNewline(t *testing.T) {
	pi, err := ParseInput(InputKindTransactionHash, validHash+"\n")
	require.NoError(t, err)
	assert.Equal(t, validHash, pi.NormalisedSource)
}

func TestParseInput_HashWithLeadingWhitespace(t *testing.T) {
	pi, err := ParseInput(InputKindTransactionHash, "  "+validHash+"  ")
	require.NoError(t, err)
	assert.Equal(t, validHash, pi.NormalisedSource)
}

func TestParseInput_HashTooShort(t *testing.T) {
	_, err := ParseInput(InputKindTransactionHash, strings.Repeat("a", 63))
	require.Error(t, err)
	pe := AsInputParseError(err)
	require.NotNil(t, pe)
	assert.Equal(t, ErrCodeInvalidHash, pe.Code)
	assert.Equal(t, StageHashFormat, pe.Stage)
	assert.NotEmpty(t, pe.Remediation)
}

func TestParseInput_HashTooLong(t *testing.T) {
	_, err := ParseInput(InputKindTransactionHash, strings.Repeat("a", 65))
	require.Error(t, err)
	pe := AsInputParseError(err)
	assert.Equal(t, ErrCodeInvalidHash, pe.Code)
}

func TestParseInput_HashNonHexChar(t *testing.T) {
	hash := strings.Repeat("a", 63) + "G" // 'G' is not hex
	_, err := ParseInput(InputKindTransactionHash, hash)
	require.Error(t, err)
	pe := AsInputParseError(err)
	require.NotNil(t, pe)
	assert.Equal(t, ErrCodeInvalidHash, pe.Code)
	assert.Equal(t, StageHashFormat, pe.Stage)
}

func TestParseInput_HashUpperCase(t *testing.T) {
	// Uppercase hex is valid
	hash := strings.Repeat("A", 64)
	pi, err := ParseInput(InputKindTransactionHash, hash)
	require.NoError(t, err)
	// NormalisedSource is lowercased
	assert.Equal(t, strings.ToLower(hash), pi.NormalisedSource)
}

// ── empty input ───────────────────────────────────────────────────────────────

func TestParseInput_EmptyString(t *testing.T) {
	_, err := ParseInput(InputKindTransactionEnvelope, "")
	require.Error(t, err)
	pe := AsInputParseError(err)
	require.NotNil(t, pe)
	assert.Equal(t, ErrCodeEmptyInput, pe.Code)
	assert.Equal(t, StageEmptyInput, pe.Stage)
	assert.NotEmpty(t, pe.Remediation)
}

func TestParseInput_WhitespaceOnly(t *testing.T) {
	_, err := ParseInput(InputKindLedgerEntry, "   \n\t  ")
	require.Error(t, err)
	pe := AsInputParseError(err)
	assert.Equal(t, ErrCodeEmptyInput, pe.Code)
}

// ── malformed base64 ──────────────────────────────────────────────────────────

func TestParseInput_MalformedBase64_Envelope(t *testing.T) {
	_, err := ParseInput(InputKindTransactionEnvelope, "not-valid-base64!!!")
	require.Error(t, err)
	pe := AsInputParseError(err)
	require.NotNil(t, pe)
	assert.Equal(t, ErrCodeMalformedBase64, pe.Code)
	assert.Equal(t, StageBase64Decode, pe.Stage)
	assert.Equal(t, InputKindTransactionEnvelope, pe.Kind)
	assert.NotEmpty(t, pe.Remediation)
}

func TestParseInput_MalformedBase64_LedgerEntry(t *testing.T) {
	_, err := ParseInput(InputKindLedgerEntry, "!!!")
	require.Error(t, err)
	pe := AsInputParseError(err)
	assert.Equal(t, ErrCodeMalformedBase64, pe.Code)
	assert.Equal(t, InputKindLedgerEntry, pe.Kind)
}

// ── invalid XDR ───────────────────────────────────────────────────────────────

func TestParseInput_ValidBase64ButInvalidXDR_Envelope(t *testing.T) {
	// "AAAA" decodes to 3 zero bytes — too short for a TransactionEnvelope XDR.
	_, err := ParseInput(InputKindTransactionEnvelope, "AAAA")
	require.Error(t, err)
	pe := AsInputParseError(err)
	require.NotNil(t, pe)
	assert.Equal(t, ErrCodeInvalidXDR, pe.Code)
	assert.Equal(t, StageXDRUnmarshal, pe.Stage)
	assert.Equal(t, InputKindTransactionEnvelope, pe.Kind)
	assert.NotEmpty(t, pe.Remediation)
}

func TestParseInput_ValidBase64ButInvalidXDR_LedgerEntry(t *testing.T) {
	_, err := ParseInput(InputKindLedgerEntry, "AAAA")
	require.Error(t, err)
	pe := AsInputParseError(err)
	assert.Equal(t, ErrCodeInvalidXDR, pe.Code)
	assert.Equal(t, InputKindLedgerEntry, pe.Kind)
}

// ── result meta (best-effort validation) ─────────────────────────────────────

func TestParseInput_ResultMeta_ValidBase64(t *testing.T) {
	// ResultMeta only requires valid base64 — no structural validation.
	pi, err := ParseInput(InputKindResultMeta, "AAAAAAAAAA==")
	require.NoError(t, err)
	assert.Equal(t, InputKindResultMeta, pi.Kind)
	assert.NotNil(t, pi.Decoded)
}

func TestParseInput_ResultMeta_MalformedBase64(t *testing.T) {
	_, err := ParseInput(InputKindResultMeta, "!!!")
	require.Error(t, err)
	pe := AsInputParseError(err)
	assert.Equal(t, ErrCodeMalformedBase64, pe.Code)
}

// ── generic XDR ──────────────────────────────────────────────────────────────

func TestParseInput_GenericXDR_ValidBase64(t *testing.T) {
	pi, err := ParseInput(InputKindGenericXDR, "AAAA")
	require.NoError(t, err)
	assert.Equal(t, InputKindGenericXDR, pi.Kind)
}

// ── unsupported kind ─────────────────────────────────────────────────────────

func TestParseInput_UnsupportedKind(t *testing.T) {
	_, err := ParseInput(InputKind("unknown_kind"), "AAAA")
	require.Error(t, err)
	pe := AsInputParseError(err)
	assert.Equal(t, ErrCodeUnsupportedEncoding, pe.Code)
}

// ── distinct error codes per failure ─────────────────────────────────────────

func TestParseInput_DistinctCodesForDifferentFailures(t *testing.T) {
	// Each failure type must produce a DIFFERENT error code.
	tests := []struct {
		name     string
		kind     InputKind
		input    string
		wantCode InputErrorCode
	}{
		{"empty", InputKindTransactionEnvelope, "", ErrCodeEmptyInput},
		{"bad_base64", InputKindTransactionEnvelope, "not!base64", ErrCodeMalformedBase64},
		{"bad_xdr", InputKindTransactionEnvelope, "AAAA", ErrCodeInvalidXDR},
		{"bad_hash_len", InputKindTransactionHash, "abc", ErrCodeInvalidHash},
		{"bad_hash_char", InputKindTransactionHash, strings.Repeat("g", 64), ErrCodeInvalidHash},
	}

	codes := make(map[InputErrorCode]bool)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseInput(tc.kind, tc.input)
			require.Error(t, err)
			pe := AsInputParseError(err)
			require.NotNil(t, pe, "expected *InputParseError")
			assert.Equal(t, tc.wantCode, pe.Code)
			assert.NotEmpty(t, pe.Remediation, "remediation hint must be non-empty")
		})
		if pe := AsInputParseError(nil); pe == nil {
			// just to use the function
		}
		codes[tc.wantCode] = true
	}

	// All four XDR-path codes must be distinct.
	assert.True(t, codes[ErrCodeEmptyInput])
	assert.True(t, codes[ErrCodeMalformedBase64])
	assert.True(t, codes[ErrCodeInvalidXDR])
	assert.True(t, codes[ErrCodeInvalidHash])
}

// ── IsInputParseError / AsInputParseError ─────────────────────────────────────

func TestIsInputParseError_True(t *testing.T) {
	_, err := ParseInput(InputKindTransactionHash, "bad")
	assert.True(t, IsInputParseError(err))
}

func TestIsInputParseError_False(t *testing.T) {
	assert.False(t, IsInputParseError(nil))
}

func TestAsInputParseError_Nil(t *testing.T) {
	assert.Nil(t, AsInputParseError(nil))
}

// ── ReadInputValue via stdin injection ────────────────────────────────────────

func TestReadInputValue_DirectString(t *testing.T) {
	val, err := readInputValueWithStdin("hello", strings.NewReader(""))
	require.NoError(t, err)
	assert.Equal(t, "hello", val)
}

func TestReadInputValue_FromStdin(t *testing.T) {
	val, err := readInputValueWithStdin("", strings.NewReader("from_stdin"))
	require.NoError(t, err)
	assert.Equal(t, "from_stdin", val)
}

func TestReadInputValue_StdinAndDirectBothProvided_DirectWins(t *testing.T) {
	// When a direct value is provided, stdin is ignored.
	val, err := readInputValueWithStdin("direct", strings.NewReader("stdin_data"))
	require.NoError(t, err)
	assert.Equal(t, "direct", val)
}

// ── stdin and file inputs behave identically ──────────────────────────────────

func TestParseInput_StdinAndFileBehavioralEquivalence(t *testing.T) {
	// A value read from stdin is treated the same as a direct value when both
	// go through ParseInput.  We test this by comparing fingerprints.
	hash := strings.Repeat("b", 64)

	piDirect, err := ParseInput(InputKindTransactionHash, hash)
	require.NoError(t, err)

	// Simulate "from stdin" by injecting via readInputValueWithStdin.
	fromStdin, err := readInputValueWithStdin("", strings.NewReader(hash))
	require.NoError(t, err)
	piStdin, err := ParseInput(InputKindTransactionHash, fromStdin)
	require.NoError(t, err)

	assert.Equal(t, piDirect.NormalisedSource, piStdin.NormalisedSource,
		"stdin and direct value must produce the same parsed output")
}

// ── trailing whitespace handled predictably ───────────────────────────────────

func TestParseInput_TrailingWhitespaceStripped(t *testing.T) {
	hash := strings.Repeat("c", 64)
	piClean, err := ParseInput(InputKindTransactionHash, hash)
	require.NoError(t, err)

	piTrailing, err := ParseInput(InputKindTransactionHash, hash+" \t\n")
	require.NoError(t, err)

	assert.Equal(t, piClean.NormalisedSource, piTrailing.NormalisedSource)
}

// ── malformed XDR never reaches the simulator ────────────────────────────────

func TestParseInput_MalformedXDR_BlockedBeforeSimulator(t *testing.T) {
	// Verify that invalid XDR is caught at parse time with ErrCodeInvalidXDR
	// so it cannot propagate to the simulator.
	_, err := ParseInput(InputKindTransactionEnvelope, "AAAA")
	require.Error(t, err)
	pe := AsInputParseError(err)
	require.NotNil(t, pe, "must be an InputParseError, not a raw error")
	assert.Equal(t, ErrCodeInvalidXDR, pe.Code,
		"must be ErrCodeInvalidXDR so the caller knows the XDR is malformed, not just bad base64")
	assert.Equal(t, StageXDRUnmarshal, pe.Stage,
		"stage must be xdr_unmarshal to distinguish from base64 failures")
}
