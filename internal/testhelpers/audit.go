// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package testhelpers

import (
	"encoding/json"
)

// AuditPayloadFixture is a minimal builder for JSON payloads passed to
// audit:sign in regression tests.
type AuditPayloadFixture struct {
	fields map[string]interface{}
}

// NewAuditPayloadFixture creates a new audit payload fixture with the
// canonical stub timestamp pre-populated.
func NewAuditPayloadFixture() *AuditPayloadFixture {
	return &AuditPayloadFixture{
		fields: map[string]interface{}{
			"input":     map[string]interface{}{},
			"state":     map[string]interface{}{},
			"events":    []interface{}{},
			"timestamp": CanonicalTimestamp,
		},
	}
}

// WithField adds or overrides a top-level field.
func (f *AuditPayloadFixture) WithField(key string, value interface{}) *AuditPayloadFixture {
	f.fields[key] = value
	return f
}

// Empty removes all fields, producing an empty JSON object.
// Use this to reproduce the "empty payload rejected" failure class.
func (f *AuditPayloadFixture) Empty() *AuditPayloadFixture {
	f.fields = map[string]interface{}{}
	return f
}

// InvalidJSON returns a raw byte slice that is not valid JSON, for testing
// the "invalid JSON payload" rejection path.
func (f *AuditPayloadFixture) InvalidJSON() []byte {
	return []byte(`{"not": "closed"`)
}

// BuildJSON marshals the fixture to a JSON byte slice.
// The marshalling is guaranteed to succeed because all field values are
// built programmatically; a panic means a bug in the test itself.
func (f *AuditPayloadFixture) BuildJSON() []byte {
	b, err := json.Marshal(f.fields)
	if err != nil {
		// Should never happen with the controlled types used here.
		panic("testhelpers.AuditPayloadFixture: unexpected JSON marshal error: " + err.Error())
	}
	return b
}

// BuildString returns the payload as a JSON string (for --payload flag usage).
func (f *AuditPayloadFixture) BuildString() string {
	return string(f.BuildJSON())
}
