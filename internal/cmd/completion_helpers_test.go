// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// ── existing completion helpers ───────────────────────────────────────────────

func TestCompleteNetworkFlag(t *testing.T) {
	completions, directive := completeNetworkFlag(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	// At minimum the 3 built-in networks must be present (custom networks are optional).
	if len(completions) < len(networkAliases) {
		t.Fatalf("expected at least %d network completions, got %d", len(networkAliases), len(completions))
	}
}

func TestCompleteInitNetworkFlag(t *testing.T) {
	completions, directive := completeInitNetworkFlag(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != 4 {
		t.Fatalf("expected 4 init network completions, got %d", len(completions))
	}
}

func TestCompleteThemeFlag(t *testing.T) {
	completions, directive := completeThemeFlag(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != len(ThemeValues) {
		t.Fatalf("expected %d theme completions, got %d", len(ThemeValues), len(completions))
	}
}

func TestCompleteXDRFormatFlag(t *testing.T) {
	completions, directive := completeXDRFormatFlag(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != len(XDRFormatValues) {
		t.Fatalf("expected %d xdr format completions, got %d", len(XDRFormatValues), len(completions))
	}
}

func TestCompleteXDRTypeFlag(t *testing.T) {
	completions, directive := completeXDRTypeFlag(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != len(XDRTypeValues) {
		t.Fatalf("expected %d xdr type completions, got %d", len(XDRTypeValues), len(completions))
	}
}

func TestCompleteReportFormatFlag(t *testing.T) {
	completions, directive := completeReportFormatFlag(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if len(completions) != len(ReportFormatValues) {
		t.Fatalf("expected %d report format completions, got %d", len(ReportFormatValues), len(completions))
	}
}

func TestCompleteNoOp(t *testing.T) {
	completions, directive := completeNoOp(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
	if completions != nil {
		t.Fatalf("expected nil completions, got %v", completions)
	}
}

// ── new completion helpers ────────────────────────────────────────────────────

func TestCompleteTraceExportFormatFlag(t *testing.T) {
	completions, directive := completeTraceExportFormatFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "trace export formats", completions, TraceExportFormatValues)
}

func TestCompleteExportFormatFlag(t *testing.T) {
	completions, directive := completeExportFormatFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "export formats", completions, ExportFormatValues)
}

func TestCompleteBindingsRuntimeFlag(t *testing.T) {
	completions, directive := completeBindingsRuntimeFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "bindings runtimes", completions, BindingsRuntimeValues)
}

func TestCompleteSpecFormatFlag(t *testing.T) {
	completions, directive := completeSpecFormatFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "spec formats", completions, SpecFormatValues)
}

func TestCompleteGeneralFormatFlag(t *testing.T) {
	completions, directive := completeGeneralFormatFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "general formats", completions, GeneralFormatValues)
}

func TestCompleteLogLevelFlag(t *testing.T) {
	completions, directive := completeLogLevelFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "log levels", completions, LogLevelValues)
}

func TestCompleteTraceVerbosityFlag(t *testing.T) {
	completions, directive := completeTraceVerbosityFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "trace verbosities", completions, TraceVerbosityValues)
}

func TestCompleteProfileFormatFlag(t *testing.T) {
	completions, directive := completeProfileFormatFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "profile formats", completions, ProfileFormatValues)
}

func TestCompleteViewModeFlag(t *testing.T) {
	completions, directive := completeViewModeFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "view modes", completions, ViewModeValues)
}

func TestCompleteFailoverStrategyFlag(t *testing.T) {
	completions, directive := completeFailoverStrategyFlag(nil, nil, "")
	assertNoFileComp(t, directive)
	assertContainsAll(t, "failover strategies", completions, FailoverStrategyValues)
}

// ── AllCompletionFunctionsReturnNoFileComp ────────────────────────────────────

// TestAllCompletersReturnNoFileComp enforces the contract that no completion
// function falls back to filename completion (which could trigger file-system
// reads or, worse, network probes through shell hooks).
func TestAllCompletersReturnNoFileComp(t *testing.T) {
	completers := map[string]func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective){
		"network":          completeNetworkFlag,
		"init-network":     completeInitNetworkFlag,
		"theme":            completeThemeFlag,
		"xdr-format":       completeXDRFormatFlag,
		"xdr-type":         completeXDRTypeFlag,
		"report-format":    completeReportFormatFlag,
		"trace-format":     completeTraceExportFormatFlag,
		"export-format":    completeExportFormatFlag,
		"bindings-runtime": completeBindingsRuntimeFlag,
		"spec-format":      completeSpecFormatFlag,
		"general-format":   completeGeneralFormatFlag,
		"log-level":        completeLogLevelFlag,
		"trace-verbosity":  completeTraceVerbosityFlag,
		"profile-format":   completeProfileFormatFlag,
		"view-mode":        completeViewModeFlag,
		"failover-strategy": completeFailoverStrategyFlag,
		"noop":             completeNoOp,
	}

	for name, fn := range completers {
		t.Run(name, func(t *testing.T) {
			_, directive := fn(nil, nil, "")
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("completer %q must return ShellCompDirectiveNoFileComp, got %v", name, directive)
			}
		})
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertNoFileComp(t *testing.T, directive cobra.ShellCompDirective) {
	t.Helper()
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
	}
}

// assertContainsAll checks that every value in wantValues appears (as a prefix)
// in at least one of the completions entries (which may carry "\tDescription").
func assertContainsAll(t *testing.T, label string, completions []string, wantValues []string) {
	t.Helper()
	for _, want := range wantValues {
		found := false
		for _, got := range completions {
			// Completion entries are either "value" or "value\tDescription".
			entry := got
			if idx := indexByte(got, '\t'); idx >= 0 {
				entry = got[:idx]
			}
			if entry == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: value %q not found in completions %v", label, want, completions)
		}
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
