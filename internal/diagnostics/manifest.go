// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package diagnostics

import (
	"os"
	"runtime"
	"time"

	"github.com/dotandev/glassbox/internal/version"
)

// ManifestSchemaVersion is incremented whenever the manifest layout changes.
const ManifestSchemaVersion = 1

// Manifest is the top-level document written as manifest.json inside every
// diagnostics archive.  Every field is safe to share publicly: secrets are
// redacted by the collector before reaching this struct.
type Manifest struct {
	// Schema identifies the manifest format version.
	SchemaVersion int `json:"schema_version"`

	// Meta contains version and build metadata.
	Meta VersionMeta `json:"meta"`

	// Platform contains OS and runtime environment details.
	Platform PlatformInfo `json:"platform"`

	// Config is the redacted configuration shape.  Values that looked like
	// secrets have been replaced with "[REDACTED]".
	Config RedactedConfig `json:"config"`

	// Checks is the list of doctor check results.
	Checks []CheckResult `json:"checks"`

	// GeneratedAt is the UTC timestamp when the archive was created.
	GeneratedAt time.Time `json:"generated_at"`
}

// VersionMeta holds build-time and runtime version information.
type VersionMeta struct {
	GlassboxVersion string `json:"glassbox_version"`
	CommitSHA       string `json:"commit_sha"`
	BuildDate       string `json:"build_date"`
	GoVersion       string `json:"go_version"`
}

// PlatformInfo describes the host operating system and architecture.
type PlatformInfo struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NumCPU       int    `json:"num_cpu"`
	GoOS         string `json:"go_os"`
	GoArch       string `json:"go_arch"`
	Hostname     string `json:"hostname,omitempty"`
	ProtocolReg  string `json:"protocol_registration_state"`
}

// RedactedConfig mirrors config.Config but uses string-typed values so that
// any token or secret-like field can be replaced with RedactedPlaceholder.
type RedactedConfig struct {
	// Non-sensitive fields are included as-is.
	RpcURL           string `json:"rpc_url"`
	Network          string `json:"network"`
	LogLevel         string `json:"log_level"`
	RequestTimeout   int    `json:"request_timeout"`
	MaxTraceDepth    int    `json:"max_trace_depth"`
	FailureThreshold int    `json:"failure_threshold"`
	RetryTimeout     int    `json:"retry_timeout"`
	FailoverStrategy string `json:"failover_strategy,omitempty"`
	Telemetry        bool   `json:"telemetry"`
	CrashReporting   bool   `json:"crash_reporting"`

	// Sensitive fields are always redacted.
	RPCToken       string `json:"rpc_token"`
	CrashSentryDSN string `json:"crash_sentry_dsn"`
	CrashEndpoint  string `json:"crash_endpoint"`

	// CachePath with home dir replaced by ~.
	CachePath string `json:"cache_path"`
}

// CheckResult is a single doctor check outcome, safe for inclusion in the
// portable bundle.
type CheckResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Version   string `json:"version,omitempty"`
	FixHint   string `json:"fix_hint,omitempty"`
	// Path is included only when it does not contain the user's home directory
	// (already masked to ~/ by RedactPath).
	Path string `json:"path,omitempty"`
}

// BuildVersionMeta constructs a VersionMeta from the current binary's build vars.
func BuildVersionMeta() VersionMeta {
	return VersionMeta{
		GlassboxVersion: version.Version,
		CommitSHA:       version.CommitSHA,
		BuildDate:       version.BuildDate,
		GoVersion:       runtime.Version(),
	}
}

// BuildPlatformInfo collects safe platform details.
func BuildPlatformInfo(protocolState string) PlatformInfo {
	hostname, _ := os.Hostname()
	// Redact hostname for privacy — only include first label (e.g. "mymac"
	// from "mymac.local") to avoid leaking domain names.
	if len(hostname) > 0 {
		for i, ch := range hostname {
			if ch == '.' {
				hostname = hostname[:i]
				break
			}
		}
	}
	return PlatformInfo{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		NumCPU:      runtime.NumCPU(),
		GoOS:        runtime.GOOS,
		GoArch:      runtime.GOARCH,
		Hostname:    hostname,
		ProtocolReg: protocolState,
	}
}
