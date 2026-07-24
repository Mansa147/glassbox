// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// InputKind names the type of replay input being parsed.
type InputKind string

const (
	InputKindTransactionEnvelope InputKind = "transaction_envelope"
	InputKindLedgerEntry         InputKind = "ledger_entry"
	InputKindResultMeta          InputKind = "result_meta"
	InputKindGenericXDR          InputKind = "generic_xdr"
	InputKindTransactionHash     InputKind = "transaction_hash"
)

// InputParseStage names the stage at which a parse failure occurred.
// Stable codes are used in error messages so tooling can key off them.
type InputParseStage string

const (
	StageWhitespaceNorm InputParseStage = "whitespace_normalisation"
	StageBase64Decode   InputParseStage = "base64_decode"
	StageXDRUnmarshal   InputParseStage = "xdr_unmarshal"
	StageHashFormat     InputParseStage = "hash_format"
	StageEmptyInput     InputParseStage = "empty_input"
	StageReadSource     InputParseStage = "read_source"
)

// InputErrorCode is a stable, unique code for each kind of input validation
// failure.  These codes appear in error messages and JSON output so that
// automated tools can react to them without parsing message text.
type InputErrorCode string

const (
	// ErrCodeEmptyInput — no bytes provided after whitespace normalisation.
	ErrCodeEmptyInput InputErrorCode = "ERR_INPUT_EMPTY"
	// ErrCodeReadFailed — the file or stdin source could not be read.
	ErrCodeReadFailed InputErrorCode = "ERR_INPUT_READ"
	// ErrCodeMalformedBase64 — the string is not valid base64.
	ErrCodeMalformedBase64 InputErrorCode = "ERR_INPUT_BASE64"
	// ErrCodeInvalidXDR — valid base64 but the XDR structure is malformed.
	ErrCodeInvalidXDR InputErrorCode = "ERR_INPUT_XDR"
	// ErrCodeInvalidHash — the string is not a 64-char hex SHA-256 hash.
	ErrCodeInvalidHash InputErrorCode = "ERR_INPUT_HASH"
	// ErrCodeEnvelopeMismatch — valid XDR envelope but the type or network
	// does not match the expected context (valid but wrong ledger).
	ErrCodeEnvelopeMismatch InputErrorCode = "ERR_INPUT_ENVELOPE_MISMATCH"
	// ErrCodeUnsupportedEncoding — the encoding type is not supported.
	ErrCodeUnsupportedEncoding InputErrorCode = "ERR_INPUT_ENCODING"
)

// InputParseError is a structured error returned by the centralized input
// parser.  It always carries a stable ErrorCode and a Remediation hint so
// users can understand and fix the problem without reading Go stack traces.
type InputParseError struct {
	// Code is the stable error code.
	Code InputErrorCode
	// Kind is the type of input that failed.
	Kind InputKind
	// Stage is the validation stage at which the error was detected.
	Stage InputParseStage
	// Message is a human-readable explanation.
	Message string
	// Remediation is an actionable suggestion for the user.
	Remediation string
	// Wrapped is the underlying error (may be nil).
	Wrapped error
}

func (e *InputParseError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %s parsing failed at stage %q: %s",
		e.Code, e.Kind, e.Stage, e.Message))
	if e.Remediation != "" {
		sb.WriteString("\n  Remediation: ")
		sb.WriteString(e.Remediation)
	}
	return sb.String()
}

func (e *InputParseError) Unwrap() error { return e.Wrapped }

// IsInputParseError reports whether err is an *InputParseError.
func IsInputParseError(err error) bool {
	_, ok := err.(*InputParseError)
	return ok
}

// AsInputParseError returns the *InputParseError if err is one, or nil.
func AsInputParseError(err error) *InputParseError {
	if pe, ok := err.(*InputParseError); ok {
		return pe
	}
	return nil
}

// inputParseErr is the internal constructor for *InputParseError.
func inputParseErr(code InputErrorCode, kind InputKind, stage InputParseStage, msg, remediation string, wrapped error) *InputParseError {
	return &InputParseError{
		Code:        code,
		Kind:        kind,
		Stage:       stage,
		Message:     msg,
		Remediation: remediation,
		Wrapped:     wrapped,
	}
}

// ParsedInput holds the result of a successful ParseInput call.
type ParsedInput struct {
	// Kind is the input kind.
	Kind InputKind
	// Raw is the raw bytes provided by the caller after whitespace normalisation.
	Raw []byte
	// Decoded is the base64-decoded payload for XDR kinds (nil for hash inputs).
	Decoded []byte
	// NormalisedSource is the whitespace-normalised input string.
	NormalisedSource string
}

// ParseInput is the single entry point for all CLI input parsing.  It:
//
//  1. Reads the input from a string, file path, or stdin (see ReadInputValue).
//  2. Normalises leading/trailing whitespace (and interior line breaks).
//  3. For XDR kinds: validates base64 encoding, then validates the XDR
//     structure, and reports exactly which stage failed with a stable error code
//     and a remediation hint.
//  4. For transaction hash inputs: validates the 64-hex-char format.
//
// Raw input bytes are never persisted beyond this function.  The caller
// receives only the decoded payload and normalised source string.
func ParseInput(kind InputKind, raw string) (*ParsedInput, error) {
	// ── 1. Normalise whitespace ───────────────────────────────────────────────
	normalised := normaliseInputString(raw)
	if normalised == "" {
		return nil, inputParseErr(
			ErrCodeEmptyInput, kind, StageEmptyInput,
			"input is empty or contains only whitespace",
			"Provide the "+string(kind)+" as a base64-encoded XDR string or a 64-character hex transaction hash.",
			nil,
		)
	}

	// ── 2. Dispatch by kind ───────────────────────────────────────────────────
	switch kind {
	case InputKindTransactionHash:
		return parseHashInput(normalised)

	case InputKindTransactionEnvelope:
		decoded, err := decodeBase64Input(kind, normalised)
		if err != nil {
			return nil, err
		}
		if err := validateEnvelopeXDR(decoded); err != nil {
			return nil, err
		}
		return &ParsedInput{Kind: kind, Raw: []byte(normalised), Decoded: decoded, NormalisedSource: normalised}, nil

	case InputKindLedgerEntry:
		decoded, err := decodeBase64Input(kind, normalised)
		if err != nil {
			return nil, err
		}
		if err := validateLedgerEntryXDR(decoded); err != nil {
			return nil, err
		}
		return &ParsedInput{Kind: kind, Raw: []byte(normalised), Decoded: decoded, NormalisedSource: normalised}, nil

	case InputKindResultMeta:
		decoded, err := decodeBase64Input(kind, normalised)
		if err != nil {
			return nil, err
		}
		// ResultMeta structural validation is best-effort — not all versions have
		// the same shape, so we only verify the base64 here.
		return &ParsedInput{Kind: kind, Raw: []byte(normalised), Decoded: decoded, NormalisedSource: normalised}, nil

	case InputKindGenericXDR:
		decoded, err := decodeBase64Input(kind, normalised)
		if err != nil {
			return nil, err
		}
		return &ParsedInput{Kind: kind, Raw: []byte(normalised), Decoded: decoded, NormalisedSource: normalised}, nil

	default:
		return nil, inputParseErr(
			ErrCodeUnsupportedEncoding, kind, StageBase64Decode,
			fmt.Sprintf("unsupported input kind %q", kind),
			"Use one of: transaction_envelope, ledger_entry, result_meta, generic_xdr, transaction_hash.",
			nil,
		)
	}
}

// ReadInputValue reads an input value from string literal, file path, or stdin.
//
//   - When value is non-empty and does not start with "@" it is returned as-is.
//   - When value starts with "@" the remainder is treated as a file path and
//     the file is read.
//   - When value is "" and stdin has data (pipe/redirect), stdin is consumed.
//   - When value is "" and stdin is a TTY, an error is returned.
//
// The raw bytes are returned; whitespace normalisation is handled by ParseInput.
func ReadInputValue(value string) (string, error) {
	return readInputValueWithStdin(value, os.Stdin)
}

// readInputValueWithStdin is the injectable version for testing.
func readInputValueWithStdin(value string, stdin io.Reader) (string, error) {
	// File path via @<path> prefix.
	if strings.HasPrefix(value, "@") {
		path := value[1:]
		if path == "" {
			return "", inputParseErr(
				ErrCodeReadFailed, InputKindGenericXDR, StageReadSource,
				`"@" prefix requires a file path (e.g. @/path/to/file.xdr)`,
				`Provide a path after "@", e.g. --envelope @envelope.xdr`,
				nil,
			)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", inputParseErr(
				ErrCodeReadFailed, InputKindGenericXDR, StageReadSource,
				fmt.Sprintf("failed to read file %q: %v", path, err),
				"Check the file path and permissions.",
				err,
			)
		}
		return string(data), nil
	}

	// Direct value.
	if value != "" {
		return value, nil
	}

	// Stdin (pipe/redirect).
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", inputParseErr(
			ErrCodeReadFailed, InputKindGenericXDR, StageReadSource,
			fmt.Sprintf("failed to inspect stdin: %v", err),
			"Pipe input via stdin or provide the value with the flag.",
			err,
		)
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return "", inputParseErr(
			ErrCodeEmptyInput, InputKindGenericXDR, StageEmptyInput,
			"no input provided and stdin is a terminal",
			"Pipe the value via stdin or pass it with the appropriate flag.",
			nil,
		)
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", inputParseErr(
			ErrCodeReadFailed, InputKindGenericXDR, StageReadSource,
			fmt.Sprintf("failed to read stdin: %v", err),
			"Ensure the piped data is complete and try again.",
			err,
		)
	}
	return string(data), nil
}

// ── private helpers ───────────────────────────────────────────────────────────

// normaliseInputString strips leading/trailing whitespace and removes any
// embedded newlines that may have been introduced by shell heredoc or multiline
// copy-paste.  Interior whitespace between tokens is not collapsed so valid
// base64 with embedded newlines (RFC 2045 MIME) is handled correctly.
func normaliseInputString(s string) string {
	// Trim standard ASCII and Unicode whitespace from both ends.
	s = strings.TrimSpace(s)
	// Remove any embedded CR/LF that are illegal in base64 and hashes.
	s = strings.ReplaceAll(s, "\r\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// decodeBase64Input validates and decodes a base64-encoded input string.
func decodeBase64Input(kind InputKind, s string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// Try URL-safe base64 as a fallback before reporting an error.
		decoded, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return nil, inputParseErr(
				ErrCodeMalformedBase64, kind, StageBase64Decode,
				fmt.Sprintf("not valid base64: %v", err),
				fmt.Sprintf(
					"The %s must be standard base64-encoded XDR (use -n flag with echo, or verify the copy-paste did not introduce whitespace).",
					kind,
				),
				err,
			)
		}
	}
	return decoded, nil
}

// validateEnvelopeXDR attempts to unmarshal bytes as a TransactionEnvelope.
func validateEnvelopeXDR(data []byte) error {
	var env xdr.TransactionEnvelope
	if err := xdr.SafeUnmarshal(data, &env); err != nil {
		return inputParseErr(
			ErrCodeInvalidXDR, InputKindTransactionEnvelope, StageXDRUnmarshal,
			fmt.Sprintf("XDR envelope unmarshal failed: %v", err),
			"Verify the base64 string is a valid TransactionEnvelope XDR produced by the Stellar SDK or stellar-cli.",
			err,
		)
	}
	return nil
}

// validateLedgerEntryXDR attempts to unmarshal bytes as a LedgerEntry.
func validateLedgerEntryXDR(data []byte) error {
	var entry xdr.LedgerEntry
	if err := xdr.SafeUnmarshal(data, &entry); err != nil {
		return inputParseErr(
			ErrCodeInvalidXDR, InputKindLedgerEntry, StageXDRUnmarshal,
			fmt.Sprintf("XDR ledger entry unmarshal failed: %v", err),
			"Verify the base64 string is a valid LedgerEntry XDR. Use 'glassbox xdr --type ledger-entry --data <b64>' to inspect the value.",
			err,
		)
	}
	return nil
}

// parseHashInput validates the transaction hash format.
func parseHashInput(s string) (*ParsedInput, error) {
	if len(s) != 64 {
		return nil, inputParseErr(
			ErrCodeInvalidHash, InputKindTransactionHash, StageHashFormat,
			fmt.Sprintf("transaction hash must be exactly 64 hex characters, got %d", len(s)),
			"A Stellar transaction hash is the SHA-256 hex digest of the transaction. Copy it from Stellar Explorer or the RPC response.",
			nil,
		)
	}
	for i, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return nil, inputParseErr(
				ErrCodeInvalidHash, InputKindTransactionHash, StageHashFormat,
				fmt.Sprintf("transaction hash contains non-hex character %q at position %d", c, i),
				"Transaction hashes use lowercase or uppercase hex digits (0-9, a-f). Check the copied value for typos.",
				nil,
			)
		}
	}
	return &ParsedInput{
		Kind:             InputKindTransactionHash,
		Raw:              []byte(s),
		NormalisedSource: strings.ToLower(s),
	}, nil
}


