// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/dotandev/glassbox/internal/errors"
	"github.com/dotandev/glassbox/internal/session"
	"github.com/dotandev/glassbox/internal/version"
	"github.com/spf13/cobra"
)

var (
	sessionImportPolicyFlag  string
	sessionImportPreviewFlag bool
)

var sessionImportCmd = &cobra.Command{
	Use:   "import <archive>",
	Short: "Import a shared session archive into the local store, resolving ID conflicts",
	Long: `Import a .gbx session archive produced by 'glassbox session share' into the
local session store.

Unlike 'glassbox session load' (which only makes the archive the active,
in-memory session), import persists the session to disk and explicitly
resolves any ID conflict with an existing local session:

  fail    reject the import, listing every conflicting field (default, safe)
  rename  keep both sessions by assigning the import a freshly generated ID
  merge   combine mergeable metadata (bookmark name, annotations) into the
          existing session

Use --preview to see what would conflict without importing anything.`,
	Example: `  # Preview conflicts before deciding a policy
  glassbox session import ./shared-session.gbx --preview

  # Default: fail on conflict, non-destructive
  glassbox session import ./shared-session.gbx

  # Keep both sessions under separate IDs
  glassbox session import ./shared-session.gbx --on-conflict rename

  # Merge annotations and bookmark into the existing session
  glassbox session import ./shared-session.gbx --on-conflict merge`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		archivePath := args[0]
		out := cmd.OutOrStdout()

		incoming, err := session.ImportArchive(archivePath)
		if err != nil {
			return fmt.Errorf("failed to read session archive: %w", err)
		}

		store, err := session.NewStore()
		if err != nil {
			return errors.WrapValidationError(fmt.Sprintf("failed to open session store: %v", err))
		}
		defer store.Close()

		if sessionImportPreviewFlag {
			existing, conflicts, err := session.DetectImportConflict(ctx, store, incoming)
			if err != nil {
				return err
			}
			if existing == nil {
				fmt.Fprintf(out, "No conflict: %q is not present in the local store.\n", incoming.ID)
				return nil
			}
			if len(conflicts) == 0 {
				fmt.Fprintf(out, "Session %q already exists locally with identical data.\n", incoming.ID)
				return nil
			}
			fmt.Fprintf(out, "Session %q already exists locally (%d field(s) differ):\n", incoming.ID, len(conflicts))
			for i, c := range conflicts {
				fmt.Fprintf(out, "  %d. [%s] existing=%q incoming=%q\n", i+1, c.Field, c.Existing, c.Incoming)
			}
			fmt.Fprintln(out, "\nRe-run with --on-conflict fail|rename|merge to apply a resolution.")
			return nil
		}

		policy, err := session.ParseImportConflictPolicy(sessionImportPolicyFlag)
		if err != nil {
			return errors.WrapValidationError(err.Error())
		}

		_ = session.RecordProvenance(incoming, session.ProvenanceImported, session.ActorUser,
			version.Version, "", fmt.Sprintf("imported from %s via '%s' policy", archivePath, policy), true)

		result, err := store.ImportSession(ctx, incoming, policy)
		if err != nil {
			return err
		}

		switch {
		case result.Existing == nil:
			fmt.Fprintf(out, "Session imported: %s\n", result.Saved.ID)
		case result.Renamed:
			fmt.Fprintf(out, "Conflict resolved by rename: imported as new session %s (existing %s unchanged)\n",
				result.Saved.ID, result.Existing.ID)
		case result.Merged:
			fmt.Fprintf(out, "Conflict resolved by merge: session %s updated with %d merged field(s)\n",
				result.Saved.ID, len(result.Conflicts))
		}

		return nil
	},
}

func init() {
	sessionImportCmd.Flags().StringVar(&sessionImportPolicyFlag, "on-conflict", "fail", "Conflict resolution policy: fail, rename, or merge")
	sessionImportCmd.Flags().BoolVar(&sessionImportPreviewFlag, "preview", false, "Show conflicts without importing anything")

	sessionCmd.AddCommand(sessionImportCmd)
}
