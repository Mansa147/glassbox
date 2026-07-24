// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package testhelpers

import (
	"time"

	"github.com/dotandev/glassbox/internal/session"
)

// SessionFixture is a minimal builder for session.Data used in regression tests.
type SessionFixture struct {
	ID            string
	TxHash        string
	Network       string
	Status        string
	CreatedAt     time.Time
	LastAccessAt  time.Time
	EnvelopeXdr   string
	ResultMetaXdr string
}

// NewSessionFixture creates a new session fixture with stub defaults.
func NewSessionFixture() *SessionFixture {
	now := time.Now()
	return &SessionFixture{
		ID:            "sess_test_" + CanonicalTxHash[:8],
		TxHash:        CanonicalTxHash,
		Network:       CanonicalNetwork,
		Status:        "active",
		CreatedAt:     now,
		LastAccessAt:  now,
		EnvelopeXdr:   CanonicalEnvelopeXDR,
		ResultMetaXdr: CanonicalEnvelopeXDR,
	}
}

// WithID sets the session ID.
func (f *SessionFixture) WithID(id string) *SessionFixture {
	f.ID = id
	return f
}

// WithTxHash sets the transaction hash.
func (f *SessionFixture) WithTxHash(hash string) *SessionFixture {
	f.TxHash = hash
	return f
}

// WithNetwork sets the network.
func (f *SessionFixture) WithNetwork(network string) *SessionFixture {
	f.Network = network
	return f
}

// WithStatus sets the status.
func (f *SessionFixture) WithStatus(status string) *SessionFixture {
	f.Status = status
	return f
}

// MissingTxHash clears the TxHash field to trigger validation failures.
func (f *SessionFixture) MissingTxHash() *SessionFixture {
	f.TxHash = ""
	return f
}

// Build converts the fixture into a session.Data.
func (f *SessionFixture) Build() *session.Data {
	return &session.Data{
		ID:            f.ID,
		TxHash:        f.TxHash,
		Network:       f.Network,
		Status:        f.Status,
		CreatedAt:     f.CreatedAt,
		LastAccessAt:  f.LastAccessAt,
		EnvelopeXdr:   f.EnvelopeXdr,
		ResultMetaXdr: f.ResultMetaXdr,
		HorizonURL:    "https://horizon-testnet.stellar.org",
		SchemaVersion: session.SchemaVersion,
	}
}
