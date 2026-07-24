// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dotandev/glassbox/internal/config"
	"github.com/dotandev/glassbox/internal/errors"
	"github.com/spf13/cobra"
)

var (
	configMigratePathFlag    string
	configMigrateDryRunFlag  bool
	configMigrateBackupFlag  bool
	configMigrateForceFlag   bool
)

var configMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate a Glassbox config file to the current schema version",
	Long: `Upgrade a Glassbox config file to schema_version ` + fmt.Sprintf("%d", config.CurrentSchemaVersion) + `.

By default the command operates on the highest-priority config file found in
the standard search path. Use --path to target a specific file.

Behaviour:
  • Pre-versioning files (no schema_version key) are treated as version 1 and
    have the key inserted automatically.
  • Already-current files are left unchanged (idempotent).
  • Files declaring a schema version higher than this binary understands are
    rejected with a clear message — upgrade Glassbox first.
  • A timestamped backup is created before any write unless --no-backup is set.
  • --dry-run previews changes without writing anything.

Exit codes:
  0  – success or nothing to do
  1  – validation / argument error
  3  – I/O failure`,

	Example: `  # Migrate the active config file (auto-detected)
  glassbox config migrate

  # Preview changes without writing
  glassbox config migrate --dry-run

  # Target a specific file
  glassbox config migrate --path ~/.glassbox/config.toml

  # Skip the automatic backup
  glassbox config migrate --no-backup`,

	RunE: func(cmd *cobra.Command, args []string) error {
		// ── 1. Resolve the target file ───────────────────────────────────────
		targetPath, err := resolveMigratePath(configMigratePathFlag)
		if err != nil {
			return err
		}

		// ── 2. Read current content ──────────────────────────────────────────
		raw, err := os.ReadFile(targetPath)
		if err != nil {
			return errors.WrapConfigError(
				fmt.Sprintf("failed to read config file %q", targetPath), err)
		}
		content := string(raw)

		// ── 3. Detect version and reject future schemas immediately ──────────
		v, vErr := config.DetectSchemaVersion(content)
		if vErr != nil {
			return errors.WrapConfigError("cannot determine schema version", vErr)
		}
		if v > config.CurrentSchemaVersion {
			return errors.WrapConfigError(
				fmt.Sprintf(
					"config file %q declares schema_version %d but this binary only supports up to %d; "+
						"upgrade Glassbox before migrating",
					targetPath, v, config.CurrentSchemaVersion,
				),
				nil,
			)
		}

		// ── 4. Run migration (pure, no I/O) ───────────────────────────────────
		migrated, result, mErr := config.MigrateConfig(content)
		if mErr != nil {
			return errors.WrapConfigError("migration failed", mErr)
		}

		// ── 5. Report diagnostics ────────────────────────────────────────────
		fmt.Fprintf(cmd.OutOrStdout(), "Config file : %s\n", targetPath)
		fmt.Fprintf(cmd.OutOrStdout(), "From version: %d\n", result.FromVersion)
		fmt.Fprintf(cmd.OutOrStdout(), "To version  : %d\n", result.ToVersion)
		for _, d := range result.Diagnostics {
			fmt.Fprintf(cmd.OutOrStdout(), "  • %s\n", d)
		}

		if !result.Changed {
			fmt.Fprintln(cmd.OutOrStdout(), "Nothing to migrate — file is already current.")
			return nil
		}

		if configMigrateDryRunFlag {
			fmt.Fprintln(cmd.OutOrStdout(), "\n[dry-run] Changes would be written:")
			fmt.Fprintln(cmd.OutOrStdout(), migrated)
			fmt.Fprintln(cmd.OutOrStdout(), "[dry-run] No files were modified.")
			return nil
		}

		// ── 6. Backup before destructive write ───────────────────────────────
		if configMigrateBackupFlag {
			backupPath := backupFilePath(targetPath)
			if err := os.WriteFile(backupPath, raw, 0600); err != nil {
				return errors.WrapConfigError(
					fmt.Sprintf("failed to create backup at %q", backupPath), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup created: %s\n", backupPath)
		}

		// ── 7. Write migrated content ────────────────────────────────────────
		if err := os.WriteFile(targetPath, []byte(migrated), 0600); err != nil {
			return errors.WrapConfigError(
				fmt.Sprintf("failed to write migrated config to %q", targetPath), err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Migration complete: %s updated to schema_version %d.\n",
			targetPath, result.ToVersion)
		return nil
	},
}

func init() {
	configMigrateCmd.Flags().StringVar(&configMigratePathFlag, "path", "",
		"Path to the config file to migrate (default: auto-detected active config file)")
	configMigrateCmd.Flags().BoolVar(&configMigrateDryRunFlag, "dry-run", false,
		"Preview the migrated output without writing any files")
	configMigrateCmd.Flags().BoolVar(&configMigrateBackupFlag, "backup", true,
		"Create a timestamped backup before rewriting the file (default: enabled)")
	configMigrateCmd.Flags().BoolVar(&configMigrateForceFlag, "no-backup", false,
		"Skip the automatic backup (alias for --backup=false)")

	// --no-backup sets configMigrateBackupFlag to false when present.
	// We use a PreRunE hook to reconcile rather than two conflicting flags.
	configMigrateCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("no-backup") && configMigrateForceFlag {
			configMigrateBackupFlag = false
		}
		return nil
	}

	configCmd.AddCommand(configMigrateCmd)
}

// resolveMigratePath returns the absolute path of the config file to migrate.
// If explicit is non-empty it is used directly (must exist). Otherwise the
// active config file from the standard search path is used. If no file is
// found an error is returned — we refuse to create a new file on migrate.
func resolveMigratePath(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", errors.WrapConfigError("cannot resolve path: "+explicit, err)
		}
		if _, statErr := os.Stat(abs); statErr != nil {
			if os.IsNotExist(statErr) {
				return "", errors.WrapConfigError(
					fmt.Sprintf("config file not found: %q", abs), nil)
			}
			return "", errors.WrapConfigError(
				fmt.Sprintf("cannot access %q", abs), statErr)
		}
		return abs, nil
	}

	// Trigger the standard file-resolution so ActiveConfigFile is set.
	cfg := &config.Config{}
	// We call loadFromFile indirectly by calling Load() but we only need the
	// path, not the fully-parsed config (validation may fail on an old file).
	// Use the exported ActiveConfigFile after a best-effort load.
	_ = cfg // Use the private method path via exported wrapper below.

	// Reload to populate ActiveConfigFile.
	_, _ = config.Load()
	active := config.ActiveConfigFile()
	if active == "" {
		return "", errors.WrapConfigError(
			"no config file found in the standard search path; "+
				"use --path to specify the file to migrate",
			nil,
		)
	}
	return active, nil
}

// backupFilePath returns a timestamped backup path alongside the original.
// For example: /home/user/.glassbox/config.toml.20260124T153045.bak
func backupFilePath(original string) string {
	ts := time.Now().UTC().Format("20060102T150405")
	return original + "." + ts + ".bak"
}
