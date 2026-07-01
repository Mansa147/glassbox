// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package protocolreg

import (
	"fmt"
	"strings"
)

// Summary returns a concise human-readable overview of the diagnostic report.
func (report *DiagnosticReport) Summary() string {
	if report == nil {
		return "Protocol registration diagnostics unavailable."
	}

	issueCount := len(report.Issues)
	switch report.Status {
	case StatusOK:
		return fmt.Sprintf(
			"Protocol handler %s:// is registered and healthy on %s.",
			report.Scheme, report.Platform,
		)
	case StatusNotRegistered:
		if issueCount == 0 {
			return fmt.Sprintf(
				"Protocol handler %s:// is not registered on %s.",
				report.Scheme, report.Platform,
			)
		}
		return fmt.Sprintf(
			"Protocol handler %s:// is not registered on %s (%d issue(s)).",
			report.Scheme, report.Platform, issueCount,
		)
	case StatusDegraded:
		return fmt.Sprintf(
			"Protocol handler %s:// is registered but degraded on %s (%d issue(s)).",
			report.Scheme, report.Platform, issueCount,
		)
	case StatusError:
		if issueCount == 0 {
			return fmt.Sprintf(
				"Protocol registration diagnostics failed on %s.",
				report.Platform,
			)
		}
		return fmt.Sprintf(
			"Protocol registration diagnostics failed on %s: %s",
			report.Platform, strings.Join(report.Issues, "; "),
		)
	default:
		return fmt.Sprintf(
			"Protocol handler %s:// status is %s on %s.",
			report.Scheme, report.Status, report.Platform,
		)
	}
}

// Summary returns a concise human-readable overview of the verification report.
func (report *VerificationReport) Summary() string {
	if report == nil {
		return "Protocol registration verification unavailable."
	}

	if len(report.Issues) == 0 {
		return fmt.Sprintf(
			"Verified %s:// protocol registration on %s (%d check(s) passed).",
			report.Scheme, report.Platform, len(report.Checks),
		)
	}
	return fmt.Sprintf(
		"Protocol verification failed on %s (%d issue(s), %d check(s) passed).",
		report.Platform, len(report.Issues), len(report.Checks),
	)
}
