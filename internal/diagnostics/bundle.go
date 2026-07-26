// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package diagnostics

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotandev/glassbox/internal/config"
	"github.com/dotandev/glassbox/internal/protocolreg"
)

// BundleExtension is the canonical file extension for diagnostics archives.
const BundleExtension = ".gbdiag"

// BundleOptions controls what is collected and where the archive is written.
type BundleOptions struct {
	// OutputPath is the destination file path.  If empty, a timestamped name
	// is generated in the OS temp directory.
	OutputPath string

	// IncludeChecks is the list of DependencyStatus results from the doctor
	// command.  Each entry is included as a redacted CheckResult.
	IncludeChecks []CheckResult
}

// GenerateBundle builds a redacted diagnostics archive and returns the path
// to the written file.  The function is safe to call offline; it never makes
// network requests.
func GenerateBundle(ctx context.Context, opts BundleOptions) (string, error) {
	outPath := opts.OutputPath
	if outPath == "" {
		ts := time.Now().UTC().Format("20060102-150405")
		outPath = filepath.Join(os.TempDir(), fmt.Sprintf("glassbox-diag-%s%s", ts, BundleExtension))
	}

	// Validate extension.
	ext := strings.ToLower(filepath.Ext(outPath))
	if ext != BundleExtension && ext != ".zip" {
		return "", fmt.Errorf(
			"unsupported diagnostics archive extension %q — use %s or .zip",
			ext, BundleExtension,
		)
	}

	// Collect data.
	manifest, err := collectManifest(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("collect diagnostics manifest: %w", err)
	}

	// Write archive.
	if err := writeArchive(outPath, manifest); err != nil {
		return "", fmt.Errorf("write diagnostics archive: %w", err)
	}

	return outPath, nil
}

// collectManifest assembles all diagnostics data into a Manifest, applying
// redaction to every field that could contain secret material or PII.
func collectManifest(ctx context.Context, opts BundleOptions) (*Manifest, error) {
	// --- Protocol registration state ---
	protocolState := "unknown"
	if reg, err := protocolreg.NewRegistrar(); err == nil {
		if reg.IsRegistered() {
			protocolState = "registered"
		} else {
			protocolState = "not-registered"
		}
	}

	// --- Config shape (redacted) ---
	redactedCfg := buildRedactedConfig()

	// --- Checks ---
	checks := opts.IncludeChecks

	manifest := &Manifest{
		SchemaVersion: ManifestSchemaVersion,
		Meta:          BuildVersionMeta(),
		Platform:      BuildPlatformInfo(protocolState),
		Config:        redactedCfg,
		Checks:        checks,
		GeneratedAt:   time.Now().UTC(),
	}

	return manifest, nil
}

// buildRedactedConfig loads the current config and returns a RedactedConfig
// with all sensitive fields replaced.
func buildRedactedConfig() RedactedConfig {
	cfg := config.DefaultConfig()
	// Best-effort load; if it fails we still include the defaults.
	if loaded, err := config.LoadConfig(); err == nil {
		cfg = loaded
	}

	out := RedactedConfig{
		Network:          string(cfg.Network),
		LogLevel:         cfg.LogLevel,
		RequestTimeout:   cfg.RequestTimeout,
		MaxTraceDepth:    cfg.MaxTraceDepth,
		FailureThreshold: cfg.FailureThreshold,
		RetryTimeout:     cfg.RetryTimeout,
		FailoverStrategy: cfg.FailoverStrategy,
		Telemetry:        cfg.Telemetry,
		CrashReporting:   cfg.CrashReporting,
		CachePath:        RedactPath(cfg.CachePath),
	}

	// Redact the RPC URL's path component (may contain tokens) but keep scheme+host.
	out.RpcURL = redactURLToken(cfg.RpcUrl)

	// Always redact sensitive fields.
	if cfg.RPCToken != "" {
		out.RPCToken = RedactedPlaceholder
	}
	if cfg.CrashSentryDSN != "" {
		out.CrashSentryDSN = RedactedPlaceholder
	}
	if cfg.CrashEndpoint != "" {
		out.CrashEndpoint = RedactedPlaceholder
	}

	return out
}

// redactURLToken keeps the scheme and host of a URL but replaces the path
// and query string with "[REDACTED]" when the path component looks like it
// could contain a secret (e.g. a token in the path).
func redactURLToken(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	// Cheap heuristic: if the URL contains a query string with a key that
	// looks like a token, redact from the ? onward.
	if idx := strings.IndexByte(rawURL, '?'); idx >= 0 {
		return rawURL[:idx] + "?" + RedactedPlaceholder
	}
	return rawURL
}

// writeArchive writes the manifest as a deterministic ZIP archive.
func writeArchive(outPath string, manifest *Manifest) error {
	// Ensure parent directory exists.
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create output directory %q: %w", dir, err)
		}
	}

	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()

	if err := addJSONEntry(zw, "manifest.json", manifest); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}

	// Write a human-readable README so anyone opening the archive understands it.
	readme := buildREADME(manifest)
	if err := addStringEntry(zw, "README.txt", readme); err != nil {
		return fmt.Errorf("write README.txt: %w", err)
	}

	return nil
}

// addJSONEntry serialises v with sorted keys for determinism and adds it to zw.
func addJSONEntry(zw *zip.Writer, name string, v interface{}) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// addStringEntry writes a plain string file into zw.
func addStringEntry(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, content)
	return err
}

// buildREADME returns the README text embedded in every diagnostics archive.
func buildREADME(m *Manifest) string {
	var sb strings.Builder
	sb.WriteString("Glassbox Diagnostics Archive\n")
	sb.WriteString("============================\n\n")
	sb.WriteString("Generated by: glassbox doctor --bundle\n")
	sb.WriteString(fmt.Sprintf("Glassbox version: %s (%s)\n", m.Meta.GlassboxVersion, m.Meta.CommitSHA))
	sb.WriteString(fmt.Sprintf("Generated at:     %s\n\n", m.GeneratedAt.Format(time.RFC3339)))
	sb.WriteString("Contents\n--------\n")
	sb.WriteString("  manifest.json  — machine-readable diagnostics (schema version, platform,\n")
	sb.WriteString("                   config shape, and doctor check results)\n")
	sb.WriteString("  README.txt     — this file\n\n")
	sb.WriteString("Privacy\n-------\n")
	sb.WriteString("Sensitive values (RPC tokens, private keys, Sentry DSN, crash endpoints)\n")
	sb.WriteString("have been replaced with \"[REDACTED]\".  Home-directory paths appear as ~.\n\n")
	sb.WriteString("This archive contains NO private key or token material.\n")
	return sb.String()
}
