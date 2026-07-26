// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package simulator

import (
	"encoding/base64"
	"fmt"

	"github.com/dotandev/glassbox/internal/errors"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// EnvelopeVariant names the decoded transaction type.
type EnvelopeVariant string

const (
	VariantV0      EnvelopeVariant = "TransactionV0"
	VariantV1      EnvelopeVariant = "TransactionV1"
	VariantFeeBump EnvelopeVariant = "FeeBumpTransaction"
)

// ErrUnsupportedVariant is returned when the envelope type is not supported by
// the simulator. The error text is stable so callers can match on it without
// relying on a sentinel.
var ErrUnsupportedVariant = errors.New("unsupported transaction envelope variant")

// DecodedSimEnvelope is the fully validated, simulator-ready representation of
// a transaction envelope. It carries everything the Rust simulator needs from
// the Go IPC layer without requiring the simulator to re-parse raw XDR.
type DecodedSimEnvelope struct {
	Variant       EnvelopeVariant
	SourceAccount string
	Fee           uint32
	Operations    []xdr.Operation
	// AuthEntries holds base64 XDR SorobanAuthorizationEntry values extracted
	// from all InvokeHostFunctionOp operations, in declaration order.
	AuthEntries []string
	// Footprint is non-nil for V1 transactions that carry SorobanTransactionData.
	Footprint *DecodedFootprint
	// InnerEnvelope is populated for FeeBump transactions.
	InnerEnvelope *DecodedSimEnvelope
}

// DecodedFootprint is the read/write footprint from the Soroban transaction ext.
type DecodedFootprint struct {
	// ReadOnly contains base64 XDR LedgerKey values for read-only footprint entries.
	ReadOnly []string
	// ReadWrite contains base64 XDR LedgerKey values for read-write footprint entries.
	ReadWrite []string
}

// DecodeEnvelopeXDR decodes a base64-encoded transaction envelope and returns
// a DecodedSimEnvelope. It supports V0, V1, and FeeBump variants; all other
// types return ErrUnsupportedVariant with a stable, human-readable message so
// callers can surface the limitation to users.
func DecodeEnvelopeXDR(xdrB64 string) (*DecodedSimEnvelope, error) {
	raw, err := base64.StdEncoding.DecodeString(xdrB64)
	if err != nil {
		return nil, fmt.Errorf("decode envelope: invalid base64: %w", err)
	}

	var env xdr.TransactionEnvelope
	if err := xdr.SafeUnmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: XDR unmarshal failed: %w", err)
	}

	switch env.Type {
	case xdr.EnvelopeTypeEnvelopeTypeTxV0:
		return decodeV0Envelope(env.MustV0())
	case xdr.EnvelopeTypeEnvelopeTypeTx:
		return decodeV1Envelope(env.MustV1())
	case xdr.EnvelopeTypeEnvelopeTypeTxFeeBump:
		return decodeFeeBumpEnvelope(env.MustFeeBump())
	default:
		return nil, fmt.Errorf("%w: %q — only TransactionV0, TransactionV1, and FeeBumpTransaction are supported",
			ErrUnsupportedVariant, env.Type)
	}
}

func decodeV0Envelope(v0 *xdr.TransactionV0Envelope) (*DecodedSimEnvelope, error) {
	if v0 == nil {
		return nil, fmt.Errorf("decode envelope: V0 envelope body is nil")
	}
	tx := v0.Tx
	source := xdr.AccountId{
		Type:    xdr.PublicKeyTypePublicKeyTypeEd25519,
		Ed25519: &tx.SourceAccountEd25519,
	}
	auth, err := extractAuthEntries(tx.Operations)
	if err != nil {
		return nil, err
	}
	return &DecodedSimEnvelope{
		Variant:       VariantV0,
		SourceAccount: source.Address(),
		Fee:           uint32(tx.Fee),
		Operations:    tx.Operations,
		AuthEntries:   auth,
	}, nil
}

func decodeV1Envelope(v1 *xdr.TransactionV1Envelope) (*DecodedSimEnvelope, error) {
	if v1 == nil {
		return nil, fmt.Errorf("decode envelope: V1 envelope body is nil")
	}
	tx := v1.Tx
	auth, err := extractAuthEntries(tx.Operations)
	if err != nil {
		return nil, err
	}
	fp, err := extractFootprint(tx)
	if err != nil {
		return nil, err
	}
	return &DecodedSimEnvelope{
		Variant:       VariantV1,
		SourceAccount: tx.SourceAccount.Address(),
		Fee:           uint32(tx.Fee),
		Operations:    tx.Operations,
		AuthEntries:   auth,
		Footprint:     fp,
	}, nil
}

func decodeFeeBumpEnvelope(fb *xdr.FeeBumpTransactionEnvelope) (*DecodedSimEnvelope, error) {
	if fb == nil {
		return nil, fmt.Errorf("decode envelope: FeeBump envelope body is nil")
	}
	inner, err := decodeFeeBumpInner(fb.Tx.InnerTx)
	if err != nil {
		return nil, fmt.Errorf("decode envelope: inner tx: %w", err)
	}
	return &DecodedSimEnvelope{
		Variant:       VariantFeeBump,
		SourceAccount: fb.Tx.FeeSource.Address(),
		Fee:           uint32(fb.Tx.Fee),
		InnerEnvelope: inner,
	}, nil
}

func decodeFeeBumpInner(inner xdr.FeeBumpTransactionInnerTx) (*DecodedSimEnvelope, error) {
	switch inner.Type {
	case xdr.EnvelopeTypeEnvelopeTypeTx:
		return decodeV1Envelope(inner.MustV1())
	default:
		return nil, fmt.Errorf("%w: inner transaction type %q", ErrUnsupportedVariant, inner.Type)
	}
}

// extractAuthEntries walks the operation list and collects all
// SorobanAuthorizationEntry values from InvokeHostFunctionOp operations.
// Each entry is returned as a base64-encoded XDR string.
func extractAuthEntries(ops []xdr.Operation) ([]string, error) {
	var entries []string
	for i, op := range ops {
		if op.Body.Type != xdr.OperationTypeInvokeHostFunction {
			continue
		}
		ihf := op.Body.InvokeHostFunctionOp
		if ihf == nil {
			continue
		}
		for j, auth := range ihf.Auth {
			b, err := auth.MarshalBinary()
			if err != nil {
				return nil, fmt.Errorf("auth entry [op %d, auth %d]: marshal failed: %w", i, j, err)
			}
			entries = append(entries, base64.StdEncoding.EncodeToString(b))
		}
	}
	return entries, nil
}

// extractFootprint extracts the Soroban transaction footprint from the V1
// transaction extension. Returns nil when the transaction carries no Soroban
// data (non-Soroban V1 transactions are valid and not an error).
func extractFootprint(tx xdr.Transaction) (*DecodedFootprint, error) {
	if tx.Ext.V != 1 || tx.Ext.SorobanData == nil {
		return nil, nil
	}
	fp := tx.Ext.SorobanData.Resources.Footprint
	roKeys, err := encodeLedgerKeys(fp.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("footprint read-only keys: %w", err)
	}
	rwKeys, err := encodeLedgerKeys(fp.ReadWrite)
	if err != nil {
		return nil, fmt.Errorf("footprint read-write keys: %w", err)
	}
	return &DecodedFootprint{ReadOnly: roKeys, ReadWrite: rwKeys}, nil
}

func encodeLedgerKeys(keys []xdr.LedgerKey) ([]string, error) {
	out := make([]string, len(keys))
	for i, k := range keys {
		b, err := k.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("ledger key [%d]: %w", i, err)
		}
		out[i] = base64.StdEncoding.EncodeToString(b)
	}
	return out, nil
}

// DecodeFootprintXDR decodes a base64-encoded XDR LedgerFootprint and returns
// the read-only and read-write key slices as base64 XDR strings.
func DecodeFootprintXDR(b64 string) (*DecodedFootprint, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode footprint: invalid base64: %w", err)
	}
	var fp xdr.LedgerFootprint
	if err := xdr.SafeUnmarshal(raw, &fp); err != nil {
		return nil, fmt.Errorf("decode footprint: XDR unmarshal failed: %w", err)
	}
	roKeys, err := encodeLedgerKeys(fp.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("decode footprint: read-only: %w", err)
	}
	rwKeys, err := encodeLedgerKeys(fp.ReadWrite)
	if err != nil {
		return nil, fmt.Errorf("decode footprint: read-write: %w", err)
	}
	return &DecodedFootprint{ReadOnly: roKeys, ReadWrite: rwKeys}, nil
}

// ValidateEnvelopeXDR performs a lightweight validity check on a
// base64-encoded envelope without fully decoding it. It returns
// ErrUnsupportedVariant for unsupported types and a descriptive error for
// malformed XDR. Use this for pre-flight validation before submitting to the
// simulator.
func ValidateEnvelopeXDR(xdrB64 string) error {
	_, err := DecodeEnvelopeXDR(xdrB64)
	return err
}
