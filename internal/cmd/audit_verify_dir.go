// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dotandev/glassbox/internal/audit"
	"github.com/dotandev/glassbox/internal/errors"
	"github.com/spf13/cobra"
)

var (
	auditVerifyDirPath string
	auditVerifyDirJSON bool
)

var auditVerifyDirCmd = &cobra.Command{
	Use:     "audit:verify-dir",
	GroupID: "utility",
	Short:   "Verify audit log segment manifests and checksum chain",
	Long: `Verify an audit log directory produced by rotating audit output.

The command checks each closed segment against its immutable manifest,
validates SHA-256 digests, and walks previous_segment_hash links so the
directory verifies as a tamper-evident chain. Missing segments and
interrupted rotations (segment without manifest, or vice versa) are reported
explicitly.

The active segment (current.jsonl) is noted when present but is not part of
the closed chain until it is rotated.

EXAMPLES
  # Verify a log directory
  glassbox audit:verify-dir --dir ./audit-logs

  # Machine-readable JSON output
  glassbox audit:verify-dir --dir ./audit-logs --json`,
	Args:    cobra.NoArgs,
	PreRunE: auditVerifyDirPreRunE,
	RunE:    runAuditVerifyDir,
}

func auditVerifyDirPreRunE(cmd *cobra.Command, args []string) error {
	if auditVerifyDirPath == "" {
		return errors.WrapCliArgumentRequired("dir")
	}
	info, err := os.Stat(auditVerifyDirPath)
	if err != nil {
		return errors.WrapValidationError(fmt.Sprintf("--dir %q is not readable: %v", auditVerifyDirPath, err))
	}
	if !info.IsDir() {
		return errors.WrapValidationError(fmt.Sprintf("--dir %q must be a directory", auditVerifyDirPath))
	}
	return nil
}

func init() {
	auditVerifyDirCmd.Flags().StringVar(&auditVerifyDirPath, "dir", "", "Path to the audit log directory (required)")
	auditVerifyDirCmd.Flags().BoolVar(&auditVerifyDirJSON, "json", false, "Output verification result as JSON")
	rootCmd.AddCommand(auditVerifyDirCmd)
}

func runAuditVerifyDir(cmd *cobra.Command, args []string) error {
	result, err := audit.VerifyDirectory(auditVerifyDirPath)
	if err != nil {
		return errors.WrapValidationError(err.Error())
	}

	if auditVerifyDirJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return errors.WrapMarshalFailed(err)
		}
	} else {
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Audit log directory: %s\n", auditVerifyDirPath)
		fmt.Fprintf(out, "Segments checked:   %d\n", result.SegmentsChecked)
		fmt.Fprintf(out, "Active present:     %v\n", result.ActivePresent)
		fmt.Fprintf(out, "Chain valid:        %v\n", result.ChainValid)
		if result.FirstSegmentHash != "" {
			fmt.Fprintf(out, "First segment hash: %s\n", result.FirstSegmentHash)
		}
		if result.LastSegmentHash != "" {
			fmt.Fprintf(out, "Last segment hash:  %s\n", result.LastSegmentHash)
		}
		if len(result.Issues) > 0 {
			fmt.Fprintln(out, "\nIssues:")
			for _, issue := range result.Issues {
				fmt.Fprintf(out, "  - %s\n", issue)
			}
		}
		if result.Valid {
			fmt.Fprintln(out, "\nResult: VALID — segment chain verified.")
		} else {
			fmt.Fprintln(out, "\nResult: INVALID — audit log directory failed verification.")
		}
	}

	if !result.Valid {
		return errors.WrapValidationError("audit log directory failed verification")
	}
	return nil
}
