// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package errors

import stdErrors "errors"

// ErstErrorCode is a unified error code type for RPC and Simulator boundaries.
type ErstErrorCode string

const (
	// General
	ErstUnknown          ErstErrorCode = "UNKNOWN"
	ErstValidationFailed ErstErrorCode = "VALIDATION_FAILED"
	ErstConfigFailed     ErstErrorCode = "CONFIG_ERROR"
	ErstArgumentRequired ErstErrorCode = "ARGUMENT_REQUIRED"
	// RPC
	ErstRPCConnectionFailed ErstErrorCode = "RPC_CONNECTION_FAILED"
	ErstRPCTimeout          ErstErrorCode = "RPC_TIMEOUT"
	ErstAllRPCFailed        ErstErrorCode = "ALL_RPC_FAILED"
	ErstRPCError            ErstErrorCode = "RPC_ERROR"
	// Simulator
	ErstSimulatorNotFound    ErstErrorCode = "SIMULATOR_NOT_FOUND"
	ErstSimulationFailed     ErstErrorCode = "SIMULATION_FAILED"
	ErstSimCrash             ErstErrorCode = "SIMULATOR_CRASH"
	ErstSimulationLogicError ErstErrorCode = "SIMULATION_LOGIC_ERROR"
	// Ledger/Network
	ErstLedgerNotFound  ErstErrorCode = "LEDGER_NOT_FOUND"
	ErstLedgerArchived  ErstErrorCode = "LEDGER_ARCHIVED"
	ErstInvalidNetwork  ErstErrorCode = "INVALID_NETWORK"
	ErstNetworkNotFound ErstErrorCode = "NETWORK_NOT_FOUND"
	// Rate limiting
	ErstRateLimitExceeded ErstErrorCode = "RATE_LIMIT_EXCEEDED"
	// Auth
	ErstTransactionNotFound ErstErrorCode = "TRANSACTION_NOT_FOUND"
	ErstUnauthorized        ErstErrorCode = "UNAUTHORIZED"
	// Source discovery
	ErstSourceDiscoveryFailed ErstErrorCode = "SOURCE_DISCOVERY_FAILED"
	// RPC response validation
	ErstRPCInvalidResponse ErstErrorCode = "RPC_INVALID_RESPONSE"
)

// ErstError wraps an error with a standardized code and preserves the original error string.
type ErstError struct {
	Code    ErstErrorCode
	Message string // human-readable message
	OrigErr error  // original error
	Hint    string // optional, actionable remediation guidance for the user
}

// WithHint attaches actionable remediation guidance to the error and returns it.
// The hint is surfaced to the user alongside the error so failures explain how
// to recover instead of leaving only a low-level message.
func (e *ErstError) WithHint(hint string) *ErstError {
	e.Hint = hint
	return e
}

func (e *ErstError) Error() string {
	if e.OrigErr != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.OrigErr.Error()
	}
	return string(e.Code) + ": " + e.Message
}

func (e *ErstError) Unwrap() error {
	return e.OrigErr
}

// Is allows errors.Is to match an ErstError against its corresponding sentinel
// errors. It checks both the codeToSentinel map (for Code* constants) and the
// errorCodeRegistry reverse mapping (for Glassbox* constants).
func (e *ErstError) Is(target error) bool {
	// Check the Code* -> sentinel mapping
	if sentinel, ok := codeToSentinel[e.Code]; ok {
		if target == sentinel {
			return true
		}
	}
	// Check the reverse of errorCodeRegistry: if the target sentinel
	// maps to the same ErstErrorCode as this error, it's a match.
	if code, ok := errorCodeRegistry[target]; ok {
		return code == e.Code
	}
	return false
}

// Registry mapping Go errors to ErstErrorCode
var errorCodeRegistry = map[error]ErstErrorCode{
	ErrTransactionNotFound:   ErstTransactionNotFound,
	ErrRPCConnectionFailed:   ErstRPCConnectionFailed,
	ErrRPCTimeout:            ErstRPCTimeout,
	ErrAllRPCFailed:          ErstAllRPCFailed,
	ErrSimulatorNotFound:     ErstSimulatorNotFound,
	ErrSimulationFailed:      ErstSimulationFailed,
	ErrSimCrash:              ErstSimCrash,
	ErrInvalidNetwork:        ErstInvalidNetwork,
	ErrMarshalFailed:         ErstValidationFailed,
	ErrUnmarshalFailed:       ErstValidationFailed,
	ErrSimulationLogicError:  ErstSimulationLogicError,
	ErrRPCError:              ErstRPCError,
	ErrValidationFailed:      ErstValidationFailed,
	ErrProtocolUnsupported:   ErstValidationFailed,
	ErrArgumentRequired:      ErstValidationFailed,
	ErrAuditLogInvalid:       ErstValidationFailed,
	ErrSessionNotFound:       ErstValidationFailed,
	ErrUnauthorized:          ErstUnauthorized,
	ErrLedgerNotFound:        ErstLedgerNotFound,
	ErrLedgerArchived:        ErstLedgerArchived,
	ErrRateLimitExceeded:     ErstRateLimitExceeded,
	ErrConfigFailed:          ErstConfigFailed,
	ErrNetworkNotFound:       ErstNetworkNotFound,
	ErrSourceDiscoveryFailed: ErstSourceDiscoveryFailed,
	ErrRPCInvalidResponse:    ErstRPCInvalidResponse,
}

// ClassifyError maps an error to an ErstError with a code and preserves the original error string.
func ClassifyError(err error) *ErstError {
	if err == nil {
		return nil
	}
	for sentinel, code := range errorCodeRegistry {
		if stdErrors.Is(err, sentinel) {
			return &ErstError{
				Code:    code,
				Message: err.Error(),
				OrigErr: err,
			}
		}
	}
	return &ErstError{
		Code:    ErstUnknown,
		Message: err.Error(),
		OrigErr: err,
	}
}
