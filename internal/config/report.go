// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

// Package config – configuration resolution report.
//
// BuildResolveReport returns a per-field breakdown of where each effective
// configuration value came from, whether a built-in default was applied, and
// whether the value was redacted for security.  This powers the
// `glassbox config show --explain` output.
package config

import (
	"fmt"
	"os"
	"strings"
)

// ValueSource enumerates the origin of a resolved configuration value.
type ValueSource string

const (
	SourceDefault     ValueSource = "default"
	SourceFile        ValueSource = "file"
	SourceEnvironment ValueSource = "environment"
	// SourceFlag indicates the value was set via a CLI flag.  BuildResolveReport
	// cannot detect this at config-load time; the caller may override the source
	// for individual fields after calling BuildResolveReport when flag values are
	// known.
	SourceFlag ValueSource = "flag"
)

// ResolvedValue describes one configuration field in the resolution report.
type ResolvedValue struct {
	// Field is the canonical TOML/JSON key name (e.g. "rpc_url").
	Field string `json:"field"`

	// EffectiveValue is the resolved value as it will be used at runtime.
	// Sensitive fields are replaced with "[redacted]".
	EffectiveValue string `json:"effective_value"`

	// Source records where the value came from (default / file / environment /
	// flag).
	Source ValueSource `json:"source"`

	// FilePath is the absolute path to the config file that supplied this value.
	// Empty when Source is not SourceFile.
	FilePath string `json:"file_path,omitempty"`

	// EnvVar is the environment variable that supplied this value.
	// Empty when Source is not SourceEnvironment.
	EnvVar string `json:"env_var,omitempty"`

	// DefaultApplied is true when the value was produced by the built-in
	// defaults assigner because no file, env, or flag supplied it.
	DefaultApplied bool `json:"default_applied"`

	// Redacted is true when the displayed EffectiveValue differs from the real
	// value because it contains a secret (API token, passphrase, DSN, etc.).
	Redacted bool `json:"redacted,omitempty"`
}

// ResolveReport is the complete per-field breakdown of the resolved config.
type ResolveReport struct {
	// Fields is the ordered list of resolved values, one entry per known config
	// field.  The order is stable so that diffing two reports is easy.
	Fields []ResolvedValue `json:"fields"`

	// ActiveFile is the highest-priority config file that was loaded, or empty
	// when no file was found.
	ActiveFile string `json:"active_file,omitempty"`

	// ConflictNotes contains human-readable notes about any deterministic
	// conflict resolutions (e.g. env var overriding a file value).
	ConflictNotes []string `json:"conflict_notes,omitempty"`
}

// fieldSpec describes one config field for the report builder.
type fieldSpec struct {
	key        string      // canonical TOML key
	envVar     string      // matching GLASSBOX_* env var, or ""
	getValue   func(*Config) string
	isSecret   bool // true → redact
	isZeroFunc func(*Config) bool // returns true when the field is at its zero value
}

// sensitiveKeys lists the field keys whose values must always be redacted.
var sensitiveKeys = map[string]bool{
	"rpc_token":       true,
	"network_passphrase": true,
	"crash_sentry_dsn": true,
}

// fieldSpecs is the canonical ordered list of all Config fields we report on.
// The order is stable; new fields should be appended at the end.
var fieldSpecs = []fieldSpec{
	{
		key:      "rpc_url",
		envVar:   "GLASSBOX_RPC_URL",
		getValue: func(c *Config) string { return c.RpcUrl },
		isZeroFunc: func(c *Config) bool { return c.RpcUrl == "" },
	},
	{
		key:    "network",
		envVar: "GLASSBOX_NETWORK",
		getValue: func(c *Config) string { return string(c.Network) },
		isZeroFunc: func(c *Config) bool { return c.Network == "" },
	},
	{
		key:      "network_passphrase",
		envVar:   "GLASSBOX_NETWORK_PASSPHRASE",
		getValue: func(c *Config) string { return c.NetworkPassphrase },
		isSecret: true,
		isZeroFunc: func(c *Config) bool { return c.NetworkPassphrase == "" },
	},
	{
		key:      "rpc_token",
		envVar:   "GLASSBOX_RPC_TOKEN",
		getValue: func(c *Config) string { return c.RPCToken },
		isSecret: true,
		isZeroFunc: func(c *Config) bool { return c.RPCToken == "" },
	},
	{
		key:    "log_level",
		envVar: "GLASSBOX_LOG_LEVEL",
		getValue: func(c *Config) string { return c.LogLevel },
		isZeroFunc: func(c *Config) bool { return c.LogLevel == "" },
	},
	{
		key:    "cache_path",
		envVar: "GLASSBOX_CACHE_PATH",
		getValue: func(c *Config) string { return c.CachePath },
		isZeroFunc: func(c *Config) bool { return c.CachePath == "" },
	},
	{
		key:    "request_timeout",
		envVar: "GLASSBOX_REQUEST_TIMEOUT",
		getValue: func(c *Config) string { return fmt.Sprintf("%d", c.RequestTimeout) },
		isZeroFunc: func(c *Config) bool { return c.RequestTimeout == 0 },
	},
	{
		key:    "max_trace_depth",
		envVar: "GLASSBOX_MAX_TRACE_DEPTH",
		getValue: func(c *Config) string { return fmt.Sprintf("%d", c.MaxTraceDepth) },
		isZeroFunc: func(c *Config) bool { return c.MaxTraceDepth == 0 },
	},
	{
		key:    "max_cache_size",
		envVar: "GLASSBOX_MAX_CACHE_SIZE",
		getValue: func(c *Config) string { return fmt.Sprintf("%d", c.MaxCacheSize) },
		isZeroFunc: func(c *Config) bool { return c.MaxCacheSize == 0 },
	},
	{
		key:    "failure_threshold",
		envVar: "GLASSBOX_FAILURE_THRESHOLD",
		getValue: func(c *Config) string { return fmt.Sprintf("%d", c.FailureThreshold) },
		isZeroFunc: func(c *Config) bool { return c.FailureThreshold == 0 },
	},
	{
		key:    "retry_timeout",
		envVar: "GLASSBOX_RETRY_TIMEOUT",
		getValue: func(c *Config) string { return fmt.Sprintf("%d", c.RetryTimeout) },
		isZeroFunc: func(c *Config) bool { return c.RetryTimeout == 0 },
	},
	{
		key:    "failover_strategy",
		envVar: "GLASSBOX_FAILOVER_STRATEGY",
		getValue: func(c *Config) string { return c.FailoverStrategy },
		isZeroFunc: func(c *Config) bool { return c.FailoverStrategy == "" },
	},
	{
		key:    "telemetry",
		envVar: "GLASSBOX_TELEMETRY",
		getValue: func(c *Config) string {
			if c.Telemetry {
				return "true"
			}
			return "false"
		},
		isZeroFunc: func(c *Config) bool { return !c.Telemetry },
	},
	{
		key:    "telemetry_anonymized",
		envVar: "GLASSBOX_TELEMETRY_ANONYMIZED",
		getValue: func(c *Config) string {
			if c.TelemetryAnonymized {
				return "true"
			}
			return "false"
		},
		isZeroFunc: func(c *Config) bool { return !c.TelemetryAnonymized },
	},
	{
		key:    "crash_reporting",
		envVar: "GLASSBOX_CRASH_REPORTING",
		getValue: func(c *Config) string {
			if c.CrashReporting {
				return "true"
			}
			return "false"
		},
		isZeroFunc: func(c *Config) bool { return !c.CrashReporting },
	},
	{
		key:    "crash_endpoint",
		envVar: "GLASSBOX_CRASH_ENDPOINT",
		getValue: func(c *Config) string { return c.CrashEndpoint },
		isZeroFunc: func(c *Config) bool { return c.CrashEndpoint == "" },
	},
	{
		key:      "crash_sentry_dsn",
		envVar:   "GLASSBOX_SENTRY_DSN",
		getValue: func(c *Config) string { return c.CrashSentryDSN },
		isSecret: true,
		isZeroFunc: func(c *Config) bool { return c.CrashSentryDSN == "" },
	},
	{
		key:    "telemetry_endpoint",
		envVar: "GLASSBOX_TELEMETRY_ENDPOINT",
		getValue: func(c *Config) string { return c.TelemetryEndpoint },
		isZeroFunc: func(c *Config) bool { return c.TelemetryEndpoint == "" },
	},
	{
		key:    "telemetry_sample_rate",
		envVar: "GLASSBOX_TELEMETRY_SAMPLE_RATE",
		getValue: func(c *Config) string { return fmt.Sprintf("%g", c.TelemetrySampleRate) },
		isZeroFunc: func(c *Config) bool { return c.TelemetrySampleRate == 0 },
	},
	{
		key:    "simulator_path",
		envVar: "GLASSBOX_SIMULATOR_PATH",
		getValue: func(c *Config) string { return c.SimulatorPath },
		isZeroFunc: func(c *Config) bool { return c.SimulatorPath == "" },
	},
}

// defaultFieldValues contains the known default value for each field key.
// A value is "default-applied" when it matches this map AND no env or file set it.
var defaultFieldValues = map[string]string{
	"log_level":            "info",
	"network":              "testnet",
	"request_timeout":      fmt.Sprintf("%d", defaultRequestTimeout),
	"max_trace_depth":      "50",
	"failure_threshold":    fmt.Sprintf("%d", defaultFailureThreshold),
	"retry_timeout":        fmt.Sprintf("%d", defaultRetryTimeout),
	"telemetry_sample_rate": "1",
}

// BuildResolveReport produces a ResolveReport for cfg.
//
// activeFile is the path returned by config.ActiveConfigFile() – the
// highest-priority TOML file that contributed values. Pass "" when no file was
// loaded.
//
// The returned report never exposes secret values in plain text: any field whose
// isSecret flag is true (rpc_token, network_passphrase, crash_sentry_dsn) is
// replaced with "[redacted]" in EffectiveValue regardless of its source.
func BuildResolveReport(cfg *Config, activeFile string) ResolveReport {
	report := ResolveReport{
		ActiveFile: activeFile,
	}

	for _, spec := range fieldSpecs {
		// Skip fields whose value is at the zero value AND have no tracked
		// default (e.g. optional string fields that were never set).
		effectiveRaw := spec.getValue(cfg)
		if spec.isZeroFunc(cfg) && !hasTrackedDefault(spec.key) {
			continue
		}

		// Determine effective display value (redact secrets).
		displayValue := effectiveRaw
		isRedacted := spec.isSecret || sensitiveKeys[spec.key]
		if isRedacted && effectiveRaw != "" {
			displayValue = "[redacted]"
		}

		rv := ResolvedValue{
			Field:          spec.key,
			EffectiveValue: displayValue,
			Redacted:       isRedacted && effectiveRaw != "",
		}

		// Determine source using the same precedence the load pipeline uses:
		//   env > file > default
		if spec.envVar != "" && os.Getenv(spec.envVar) != "" {
			rv.Source = SourceEnvironment
			rv.EnvVar = spec.envVar

			// Conflict note when the file also had a value that env overrides.
			if activeFile != "" && !spec.isZeroFunc(cfg) {
				fileVal := resolveFileValue(spec.key, activeFile)
				if fileVal != "" {
					envVal := os.Getenv(spec.envVar)
					if fileVal != envVal {
						report.ConflictNotes = append(report.ConflictNotes,
							fmt.Sprintf("%s: env var %s=%q overrides file value %q from %s",
								spec.key, spec.envVar, maskIfSecret(spec, envVal), maskIfSecret(spec, fileVal), activeFile))
					}
				}
			}
		} else if activeFile != "" && fileHasKey(activeFile, spec.key) {
			rv.Source = SourceFile
			rv.FilePath = activeFile
		} else {
			rv.Source = SourceDefault
			rv.DefaultApplied = isBuiltinDefault(spec.key, effectiveRaw)
		}

		report.Fields = append(report.Fields, rv)
	}

	return report
}

// hasTrackedDefault returns true when we have a known default for this key.
func hasTrackedDefault(key string) bool {
	_, ok := defaultFieldValues[key]
	return ok
}

// isBuiltinDefault returns true when the value matches the known built-in
// default for that field.
func isBuiltinDefault(key, value string) bool {
	def, ok := defaultFieldValues[key]
	if !ok {
		return false
	}
	return strings.TrimSpace(value) == strings.TrimSpace(def)
}

// fileHasKey returns true when the TOML file at path contains an assignment for
// the given key.  It performs a lightweight line scan so it does not need to
// fully parse the file.
func fileHasKey(path, key string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	prefix := key + " ="
	altPrefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) || strings.HasPrefix(trimmed, altPrefix) {
			return true
		}
	}
	return false
}

// resolveFileValue extracts the raw string value for key from the TOML file at
// path.  Returns "" when the key is not found or the file cannot be read.
func resolveFileValue(key, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	prefix := key + " ="
	altPrefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		var rest string
		if strings.HasPrefix(trimmed, prefix) {
			rest = strings.TrimPrefix(trimmed, prefix)
		} else if strings.HasPrefix(trimmed, altPrefix) {
			rest = strings.TrimPrefix(trimmed, altPrefix)
		} else {
			continue
		}
		return strings.Trim(strings.TrimSpace(rest), `"'`)
	}
	return ""
}

// maskIfSecret returns "[redacted]" when spec.isSecret is true, otherwise the
// raw value.  Used only in conflict notes so secrets stay out of log messages.
func maskIfSecret(spec fieldSpec, val string) string {
	if spec.isSecret || sensitiveKeys[spec.key] {
		return "[redacted]"
	}
	return val
}
