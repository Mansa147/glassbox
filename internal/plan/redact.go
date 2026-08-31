// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"net/url"
	"strings"
)

// redactURL removes authentication tokens and credentials from a URL before
// embedding it in a plan. It strips the password, any "token" or "key" query
// parameters, and the Authorization header value from URLs that may contain
// bearer tokens as path segments (e.g. /api/<token>/…).
//
// The host, scheme, and path structure are preserved for readability.
func redactURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		// If we can't parse it, return a generic placeholder rather than
		// exposing potentially sensitive content.
		return "<url>"
	}

	// Strip username:password from authority component.
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.User(parsed.User.Username())
		}
	}

	// Redact sensitive query parameters.
	q := parsed.Query()
	changed := false
	sensitiveParams := []string{
		"token", "key", "secret", "password", "access_token",
		"api_key", "apikey", "auth", "authorization",
	}
	for _, param := range sensitiveParams {
		for k := range q {
			if strings.EqualFold(k, param) {
				q.Set(k, "REDACTED")
				changed = true
			}
		}
	}
	if changed {
		parsed.RawQuery = q.Encode()
	}

	return parsed.String()
}

// redactKeyIdentifier redacts raw key material from a key identifier string.
// If the value looks like a hex-encoded private key (≥ 64 hex chars), PEM
// content, or a raw passphrase it is replaced with REDACTED. Safe identifiers
// such as PKCS#11 CKA_LABEL strings, KMS ARNs, and key fingerprints are
// passed through unchanged.
func redactKeyIdentifier(id string) string {
	if id == "" {
		return ""
	}

	// PEM blocks contain sensitive key material.
	if strings.Contains(id, "-----BEGIN") {
		return "REDACTED (PEM key)"
	}

	// Hex string of ≥ 64 chars is likely a raw Ed25519 seed or private key.
	trimmed := strings.TrimSpace(id)
	if len(trimmed) >= 64 && isHex(trimmed) {
		return "REDACTED (hex key)"
	}

	return id
}

// isHex returns true when all characters in s are valid hexadecimal digits.
func isHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
