// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dotandev/glassbox/internal/abi"
)

type exportState struct {
	Step             int
	Summary          string
	Operation        string
	EventType        string
	Contract         string
	Function         string
	ContractMetadata *abi.ContractMetadata
	Args             string
	Return           string
	Error            string
	SourceFile       string
	SourceLine       int
	GitHubLink       string
	CostSummary      string
	CostBreakdown    []string
	Details          []string
}

type exportData struct {
	TransactionHash string
	StartTime       string
	EndTime         string
	TotalSteps      int
	Annotations     TraceAnnotations
	States          []exportState
}

type ExportOptions struct {
	Comments        []string
	SessionMetadata map[string]string
}

const traceHTMLTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Glassbox Trace Export</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 0; padding: 1rem; background: #0b1220; color: #e8eef9; }
    .header { border-bottom: 1px solid #334155; padding-bottom: 1rem; margin-bottom: 1rem; }
    .header h1 { margin: 0 0 .25rem; font-size: 1.6rem; }
    .header p { margin: .25rem 0; color: #94a3b8; }
    .controls { margin: .75rem 0; }
    .controls button { margin-right: .5rem; padding: .5rem .75rem; border: none; border-radius: .375rem; cursor: pointer; background: #334155; color: #f8fafc; }
    details { margin-bottom: .75rem; border: 1px solid #334155; border-radius: .5rem; background: #111827; padding: .75rem; }
    summary { font-size: 1rem; font-weight: 700; cursor: pointer; }
    .state-meta { margin-top: .5rem; color: #cbd5e1; }
    .state-meta span { display: inline-block; margin-right: 1rem; }
    .field { margin: .35rem 0; }
    .field strong { color: #e2e8f0; }
    a { color: #60a5fa; }
    code { display: inline-block; background: #1e293b; padding: .15rem .3rem; border-radius: .25rem; }
  </style>
</head>
<body>
  <div class="header">
    <h1>Glassbox Trace Export</h1>
    <p>Transaction: {{ .TransactionHash }}</p>
    <p>Steps: {{ .TotalSteps }} · Started: {{ .StartTime }} · Ended: {{ .EndTime }}</p>
    {{ if .Annotations.Comments }}
    <div class="field"><strong>Comments:</strong><ul>{{ range .Annotations.Comments }}<li>{{ . }}</li>{{ end }}</ul></div>
    {{ end }}
    {{ if .Annotations.SessionMetadata }}
    <div class="field"><strong>Session metadata:</strong><ul>{{ range $k, $v := .Annotations.SessionMetadata }}<li><code>{{ $k }}</code>: {{ $v }}</li>{{ end }}</ul></div>
    {{ end }}
    <div class="controls">
      <button onclick="setAll(true)">Expand all</button>
      <button onclick="setAll(false)">Collapse all</button>
    </div>
  </div>
  {{ range .States }}
  <details open>
    <summary>#{{ .Step }} · {{ .Summary }}</summary>
    <div class="state-meta">
      <span><strong>Operation:</strong> {{ .Operation }}</span>
      {{ if .EventType }}<span><strong>Event:</strong> {{ .EventType }}</span>{{ end }}
      {{ if .Contract }}<span><strong>Contract:</strong> {{ .Contract }}</span>{{ end }}
      {{ if .Function }}<span><strong>Function:</strong> {{ .Function }}</span>{{ end }}
      {{ if .SourceFile }}<span><strong>Source:</strong> {{ .SourceFile }}:{{ .SourceLine }}</span>{{ end }}
      {{ if .GitHubLink }}<span><strong>Link:</strong> <a href="{{ .GitHubLink }}" target="_blank">View on GitHub</a></span>{{ end }}
    </div>
    <div class="field"><strong>Arguments:</strong> <code>{{ .Args }}</code></div>
    {{ if .Return }}<div class="field"><strong>Return:</strong> <code>{{ .Return }}</code></div>{{ end }}
    {{ if .Error }}<div class="field"><strong>Error:</strong> <code>{{ .Error }}</code></div>{{ end }}
    {{ if .CostSummary }}<div class="field"><strong>Cost:</strong> <code>{{ .CostSummary }}</code></div>{{ end }}
    {{ if .CostBreakdown }}
    <div class="field"><strong>Cost breakdown:</strong>
      <ul>
      {{ range .CostBreakdown }}<li>{{ . }}</li>{{ end }}
      </ul>
    </div>
    {{ end }}
    {{ if .Details }}
    <div class="field"><strong>Details:</strong>
      <ul>
      {{ range .Details }}<li>{{ . }}</li>{{ end }}
      </ul>
    </div>
    {{ end }}
  </details>
  {{ end }}
  <script>
    function setAll(open) {
      document.querySelectorAll('details').forEach(function(element) { element.open = open; });
    }
  </script>
</body>
</html>`

const traceMarkdownTemplate = `# Glassbox Trace Export

**Transaction:** {{ .TransactionHash }}

**Steps:** {{ .TotalSteps }}

**Started:** {{ .StartTime }}

**Ended:** {{ .EndTime }}

{{ if .Annotations.Comments }}## Comments
{{ range .Annotations.Comments }}- {{ . }}
{{ end }}
{{ end }}{{ if .Annotations.SessionMetadata }}## Session Metadata
{{ range $k, $v := .Annotations.SessionMetadata }}- **{{ $k }}:** {{ $v }}
{{ end }}
{{ end }}
{{ range .States }}
## Step {{ .Step }}: {{ .Summary }}

- **Operation:** {{ .Operation }}
{{ if .EventType }}- **Event:** {{ .EventType }}
{{ end }}{{ if .Contract }}- **Contract:** {{ .Contract }}
{{ end }}{{ if .Function }}- **Function:** {{ .Function }}
{{ end }}{{ if .SourceFile }}- **Source:** {{ .SourceFile }}:{{ .SourceLine }}
{{ end }}{{ if .GitHubLink }}- **GitHub:** [View on GitHub]({{ .GitHubLink }})
{{ end }}- **Arguments:** {{ .Args }}
{{ if .Return }}- **Return:** {{ .Return }}
{{ end }}{{ if .Error }}- **Error:** {{ .Error }}
{{ end }}{{ if .CostSummary }}- **Cost:** {{ .CostSummary }}
{{ end }}{{ if .CostBreakdown }}- **Cost breakdown:**
  {{ range .CostBreakdown }}
  - {{ . }}
  {{ end }}
{{ end }}{{ if .Details }}- **Details:**
  {{ range .Details }}
  - {{ . }}
  {{ end }}
{{ end }}

{{ end }}`

// ValidateTraceExportParams performs comprehensive validation of trace export parameters.
// It checks the trace, format, output path, and export options for validity before export.
// Returns an error if any validation check fails, with clear and actionable guidance.
// All failures are collected and returned together so callers can fix them in one pass.
func ValidateTraceExportParams(trace *ExecutionTrace, format string, outputPath string, opts ExportOptions) error {
	var validationErrors []string

	// 1. Validate trace is not nil
	if trace == nil {
		validationErrors = append(validationErrors, "trace is nil — cannot export a nil trace\n"+
			"  Fix: ensure a valid trace object is provided to the export function\n"+
			"  This typically means the trace failed to load or deserialize correctly")
	} else {
		// 2. Validate trace has states (no steps recorded means nothing to export)
		if len(trace.States) == 0 {
			validationErrors = append(validationErrors, "trace has no execution states (no steps recorded) — empty trace cannot be exported\n"+
				"  Possible causes:\n"+
				"    - Simulation did not produce any diagnostic events\n"+
				"    - Transaction envelope is invalid\n"+
				"    - Simulator version is incompatible\n"+
				"  Fix: verify that the trace was captured correctly and contains at least one step\n"+
				"  Tip: check that the traced transaction actually executed any code")
		}

		// 3. Validate transaction hash is present
		if strings.TrimSpace(trace.TransactionHash) == "" {
			validationErrors = append(validationErrors, "trace has no transaction hash — transaction context is missing\n"+
				"  Fix: ensure the trace was created with a valid transaction hash\n"+
				"  This is usually set automatically when loading a trace from a file")
		}

		// 4. Validate time fields are sensible
		if trace.StartTime.IsZero() {
			validationErrors = append(validationErrors, "trace start time is zero — missing temporal context\n"+
				"  Fix: verify the trace was properly initialized with a start timestamp")
		}
		if trace.EndTime.IsZero() {
			validationErrors = append(validationErrors, "trace end time is zero — missing temporal context\n"+
				"  Fix: verify the trace was properly finalized with an end timestamp")
		}
		if !trace.StartTime.IsZero() && !trace.EndTime.IsZero() && trace.EndTime.Before(trace.StartTime) {
			validationErrors = append(validationErrors, "trace end time is before start time — invalid temporal ordering\n"+
				"  Fix: verify the trace timestamps were recorded correctly\n"+
				"  Start: "+trace.StartTime.String()+", End: "+trace.EndTime.String())
		}
	}

	// 5. Validate format string
	if strings.TrimSpace(format) == "" {
		validationErrors = append(validationErrors, "--export-format is empty (format is empty) — must specify a format\n"+
			"  Fix: use --export-format with one of: html, markdown, json, text\n"+
			"  Default is html if not specified during export")
	} else {
		normalizedFormat := strings.ToLower(strings.TrimSpace(format))
		validFormats := map[string]bool{"md": true}
		for _, f := range SupportedExportFormats() {
			validFormats[f] = true
		}
		if !validFormats[normalizedFormat] {
			validationErrors = append(validationErrors, fmt.Sprintf(
				"invalid or unsupported --export-format %q — must be one of: html, markdown, json, text\n"+
					"  Fix: use a supported format\n"+
					"  html     — interactive HTML (best for browsers)\n"+
					"  markdown — GitHub-friendly markdown report\n"+
					"  json     — machine-readable JSON\n"+
					"  text     — plain text ASCII output",
				format))
		}
	}

	// 6. Validate output path
	if strings.TrimSpace(outputPath) == "" {
		validationErrors = append(validationErrors, "output path is empty — must specify a target file\n"+
			"  Fix: provide an output file path (e.g., ./trace.html or /tmp/report.md)\n"+
			"  Example: glassbox trace --export ./output/trace.html --format html input.json")
	} else {
		// Check for directory-like paths (ending with / or \)
		if strings.HasSuffix(outputPath, "/") || strings.HasSuffix(outputPath, "\\") {
			validationErrors = append(validationErrors, fmt.Sprintf(
				"output path %q looks like a directory path (ends with %q); provide a full file path\n"+
					"  Fix: specify a complete filename\n"+
					"  Example: --export ./output/trace.html instead of --export ./output/",
				outputPath, string(outputPath[len(outputPath)-1])))
		}

		// Null bytes in paths are a shell-injection risk.
		if strings.ContainsRune(outputPath, 0) {
			validationErrors = append(validationErrors, "output path contains null bytes — invalid file path\n"+
				"  Fix: remove any null bytes from the path")
		}

		// Check parent directory viability (can surface permission issues early)
		parentDir := filepath.Dir(outputPath)
		if parentDir != "." && parentDir != "" {
			if info, err := os.Stat(parentDir); err != nil {
				if os.IsPermission(err) {
					validationErrors = append(validationErrors, fmt.Sprintf(
						"output directory %q is not accessible — permission denied\n"+
							"  Fix: ensure you have read and execute permissions on the parent directory\n"+
							"  Check: ls -ld %s", parentDir, parentDir))
				}
				// Non-existent parent directory is OK — created at export time.
			} else if !info.IsDir() {
				validationErrors = append(validationErrors, fmt.Sprintf(
					"output path parent %q exists but is not a directory — invalid path\n"+
						"  Fix: provide a path where the parent is a directory\n"+
						"  Check: ls -l %s", parentDir, parentDir))
			}
		}
	}

	// 7. Validate ExportOptions.Comments count and length
	if len(opts.Comments) > 100 {
		validationErrors = append(validationErrors, fmt.Sprintf(
			"too many comments (%d) — maximum is 100 comments per trace export\n"+
				"  Fix: reduce the number of comments or split into multiple exports",
			len(opts.Comments),
		))
	}
	for i, comment := range opts.Comments {
		if len(comment) > 10000 {
			validationErrors = append(validationErrors, fmt.Sprintf(
				"comment #%d exceeds maximum length of 10000 characters (got %d)\n"+
					"  Fix: shorten the comment or split it into multiple shorter comments",
				i+1, len(comment),
			))
		} else if strings.TrimSpace(comment) == "" {
			validationErrors = append(validationErrors, fmt.Sprintf(
				"--comment index %d is empty or whitespace-only\n"+
					"  Fix: provide non-empty comments or omit empty ones", i))
		}
	}

	// 8. Validate ExportOptions.SessionMetadata keys and values
	for key, value := range opts.SessionMetadata {
		if strings.TrimSpace(key) == "" {
			validationErrors = append(validationErrors, "session metadata key is empty or whitespace-only\n"+
				"  Fix: provide non-empty keys for all metadata entries")
		}
		if strings.TrimSpace(value) == "" {
			validationErrors = append(validationErrors, fmt.Sprintf(
				"session metadata value for key %q is empty or whitespace-only\n"+
					"  Fix: provide non-empty values or omit the metadata entry", key))
		}
	}

	// Return aggregated errors if any
	if len(validationErrors) > 0 {
		if len(validationErrors) == 1 {
			return fmt.Errorf("%s", validationErrors[0])
		}
		msg := fmt.Sprintf("%d trace export validation error(s):\n", len(validationErrors))
		for i, err := range validationErrors {
			msg += fmt.Sprintf("  %d. %s\n", i+1, err)
		}
		return fmt.Errorf("%s", strings.TrimRight(msg, "\n"))
	}

	return nil
}

// ValidateTraceFormatCompatibility checks if the trace data is suitable for the target export format.
// Some formats have specific requirements or may produce suboptimal results with certain trace data.
// Returns an error if the trace is fundamentally incompatible with the format, or nil if compatible.
func ValidateTraceFormatCompatibility(trace *ExecutionTrace, format string) error {
	if trace == nil {
		return fmt.Errorf("trace is nil — cannot check format compatibility")
	}

	normalizedFormat := strings.ToLower(strings.TrimSpace(format))

	// Format-specific validation checks
	switch normalizedFormat {
	case "html":
		// HTML format handles most traces well, but check for problematic sizes
		// Large traces may cause browser rendering issues
		if len(trace.States) > 50000 {
			return fmt.Errorf(
				"trace has %d steps — too large for HTML export (browser may become unresponsive)\n"+
					"  Fix: use --format json for large traces or filter the trace verbosity\n"+
					"  Alternatively: use --trace-verbosity summary to reduce output size",
				len(trace.States))
		}

		// Check for extremely large step details that could break HTML rendering.
		// This covers both long error/operation strings and very large argument lists.
		maxDetailSize := 1000000 // 1MB
		for i, state := range trace.States {
			detailSize := len(state.Error) + len(state.Operation) + len(state.Function)
			if detailSize > maxDetailSize {
				return fmt.Errorf(
					"trace step %d has excessively large detail fields (%d bytes total) — incompatible with HTML export\n"+
						"  Fix: use --format json for traces with large step details",
					i, detailSize)
			}
			// Arguments are rendered as a single string in HTML; check their
			// serialised size so the browser doesn't receive a multi-megabyte payload.
			argStr := fmt.Sprintf("%v", state.Arguments)
			if len(argStr) > 50000 {
				return fmt.Errorf(
					"trace step %d has very large arguments (%d chars) that may cause browser rendering issues — incompatible with HTML export\n"+
						"  Fix: use --format json for traces with large argument payloads",
					i, len(argStr))
			}
		}

	case "markdown", "md":
		// Markdown format requires careful handling of special characters
		if len(trace.States) > 10000 {
			return fmt.Errorf(
				"trace has %d steps — markdown output will be extremely long (>1MB) and difficult to view\n"+
					"  Fix: use --format json for large traces or filter the trace verbosity",
				len(trace.States))
		}

		// Check for problematic markdown characters in error messages
		for i, state := range trace.States {
			if strings.Count(state.Error, "```") > 0 {
				return fmt.Errorf(
					"trace step %d error contains markdown code fence markers (`) — may break markdown formatting\n"+
						"  This is usually OK and will be handled gracefully, but you may want to review the step details",
					i)
			}
		}

	case "json":
		// JSON format is most flexible, but validate structural correctness
		// that would prevent successful serialization or round-trip loading.

		// Step indices must be sequential — a mismatch indicates a corrupted
		// or externally-modified trace that cannot be reloaded reliably.
		for i, state := range trace.States {
			if state.Step != i {
				return fmt.Errorf(
					"trace step mismatch at position %d: expected step %d but got %d — trace may be corrupted\n"+
						"  Fix: re-export or sanitize the trace before converting to JSON\n"+
						"  Tip: use ExportWithResilience to auto-repair step indices",
					i, i, state.Step)
			}
		}

	case "text":
		// Plain text format is permissive but may produce very large files
		if len(trace.States) > 100000 {
			return fmt.Errorf(
				"trace has %d steps — plain text export will be extremely large (likely >5MB) and slow to generate\n"+
					"  Fix: use --format json for very large traces or filter the trace verbosity",
				len(trace.States))
		}

	default:
		return fmt.Errorf(
			"unsupported trace export format: %q\n"+
				"  Supported formats: html, markdown, json, text\n"+
				"  Fix: use --export-format with one of the supported values",
			format)
	}

	return nil
}

// SupportedExportFormats returns the canonical list of trace export formats.
// "md" is accepted as an alias for "markdown" but is not listed separately.
//
// Adding a format here requires wiring it into ExportExecutionTraceWithOptions,
// ValidateTraceFormatCompatibility, and the printer golden tests
// (internal/trace/printer_golden_test.go) — the golden coverage test fails
// until every listed format has checked-in golden fixtures.
func SupportedExportFormats() []string {
	return []string{"html", "markdown", "json", "text"}
}

func ExportExecutionTrace(trace *ExecutionTrace, format string, outputPath string) error {
	return ExportExecutionTraceWithOptions(trace, format, outputPath, ExportOptions{})
}

func ExportExecutionTraceWithOptions(trace *ExecutionTrace, format string, outputPath string, opts ExportOptions) error {
	// Comprehensive pre-flight validation
	if err := ValidateTraceExportParams(trace, format, outputPath, opts); err != nil {
		return fmt.Errorf("trace export validation failed: %w", err)
	}

	// Format compatibility check
	if err := ValidateTraceFormatCompatibility(trace, format); err != nil {
		return fmt.Errorf("trace format compatibility check failed: %w", err)
	}

	// Accuracy and context audit — surface warnings before writing the file
	// so users can see degraded-accuracy conditions without the export failing.
	if err := ValidateTraceAccuracy(trace); err != nil {
		if te, ok := err.(*TraceInputError); ok {
			for _, f := range te.Failures {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", f)
			}
		}
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "html"
	}

	var content string
	var err error
	switch format {
	case "html":
		content, err = GenerateTraceHTMLWithOptions(trace, opts)
		if err != nil {
			return fmt.Errorf("failed to generate HTML trace: %w\n"+
				"  This may indicate invalid trace data or a template rendering error\n"+
				"  Check that all trace fields are properly populated", err)
		}
	case "markdown", "md":
		content, err = GenerateTraceMarkdownWithOptions(trace, opts)
		if err != nil {
			return fmt.Errorf("failed to generate Markdown trace: %w\n"+
				"  This may indicate invalid trace data or a template rendering error", err)
		}
	case "json":
		// For JSON, we marshal the trace directly
		jsonBytes, jsonErr := json.MarshalIndent(trace, "", "  ")
		if jsonErr != nil {
			return fmt.Errorf("failed to marshal trace as JSON: %w\n"+
				"  This indicates the trace contains non-serializable data\n"+
				"  Check for circular references or invalid field values", jsonErr)
		}
		content = string(jsonBytes)
	case "text":
		content, err = GenerateTracePlainTextWithOptions(trace, opts)
		if err != nil {
			return fmt.Errorf("failed to generate plain text trace: %w", err)
		}
	default:
		return fmt.Errorf("unsupported trace export format: %s\n"+
			"  Supported formats: html, markdown, json, text\n"+
			"  Fix: use --format with one of the supported values", format)
	}
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("failed to create trace export directory: %w\n"+
			"  Directory: %s\n"+
			"  Fix: ensure you have write permissions to the parent directory\n"+
			"  Or choose a different output path with --trace-output", err, filepath.Dir(outputPath))
	}

	// Write the file
	if err := os.WriteFile(outputPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write trace export file: %w\n"+
			"  Path: %s\n"+
			"  Fix: ensure you have write permissions and sufficient disk space\n"+
			"  Check: ls -la %s", err, outputPath, filepath.Dir(outputPath))
	}

	return nil
}

func GenerateTraceHTML(trace *ExecutionTrace) (string, error) {
	return GenerateTraceHTMLWithOptions(trace, ExportOptions{})
}

func GenerateTraceHTMLWithOptions(trace *ExecutionTrace, opts ExportOptions) (string, error) {
	if trace == nil {
		return "", fmt.Errorf("trace is nil")
	}
	annotations := mergeTraceAnnotations(trace.Annotations, opts)

	data := exportData{
		TransactionHash: trace.TransactionHash,
		StartTime:       trace.StartTime.Format(time.RFC3339),
		EndTime:         trace.EndTime.Format(time.RFC3339),
		TotalSteps:      len(trace.States),
		Annotations:     annotations,
		States:          buildExportStates(trace),
	}

	tmpl, err := template.New("trace-html").Parse(traceHTMLTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse trace export template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render trace HTML: %w", err)
	}
	return buf.String(), nil
}

func GenerateTraceMarkdown(trace *ExecutionTrace) (string, error) {
	return GenerateTraceMarkdownWithOptions(trace, ExportOptions{})
}

func GenerateTraceMarkdownWithOptions(trace *ExecutionTrace, opts ExportOptions) (string, error) {
	if trace == nil {
		return "", fmt.Errorf("trace is nil")
	}
	annotations := mergeTraceAnnotations(trace.Annotations, opts)

	data := exportData{
		TransactionHash: trace.TransactionHash,
		StartTime:       trace.StartTime.Format(time.RFC3339),
		EndTime:         trace.EndTime.Format(time.RFC3339),
		TotalSteps:      len(trace.States),
		Annotations:     annotations,
		States:          buildExportStates(trace),
	}

	tmpl, err := template.New("trace-md").Parse(traceMarkdownTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse trace export template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render trace markdown: %w", err)
	}
	return buf.String(), nil
}

// GenerateTracePlainText renders a shareable plain-text trace with indented hierarchy.
func GenerateTracePlainText(trace *ExecutionTrace) (string, error) {
	return GenerateTracePlainTextWithOptions(trace, ExportOptions{})
}

// GenerateTracePlainTextWithOptions renders a plain-text trace including any
// comments and session metadata, matching the semantic coverage of the HTML
// and Markdown exporters (previously the text format silently dropped
// annotations).
func GenerateTracePlainTextWithOptions(trace *ExecutionTrace, opts ExportOptions) (string, error) {
	if trace == nil {
		return "", fmt.Errorf("trace is nil")
	}
	annotations := mergeTraceAnnotations(trace.Annotations, opts)

	data := exportData{
		TransactionHash: trace.TransactionHash,
		StartTime:       trace.StartTime.Format(time.RFC3339),
		EndTime:         trace.EndTime.Format(time.RFC3339),
		TotalSteps:      len(trace.States),
		Annotations:     annotations,
		States:          buildExportStates(trace),
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "Glassbox Trace Export\n")
	fmt.Fprintf(&buf, "=====================\n\n")
	fmt.Fprintf(&buf, "Transaction: %s\n", data.TransactionHash)
	fmt.Fprintf(&buf, "Steps:       %d\n", data.TotalSteps)
	fmt.Fprintf(&buf, "Started:     %s\n", data.StartTime)
	fmt.Fprintf(&buf, "Ended:       %s\n\n", data.EndTime)

	if len(data.Annotations.Comments) > 0 {
		buf.WriteString("Comments:\n")
		for _, c := range data.Annotations.Comments {
			fmt.Fprintf(&buf, "  - %s\n", c)
		}
		buf.WriteString("\n")
	}
	if len(data.Annotations.SessionMetadata) > 0 {
		keys := make([]string, 0, len(data.Annotations.SessionMetadata))
		for k := range data.Annotations.SessionMetadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteString("Session metadata:\n")
		for _, k := range keys {
			fmt.Fprintf(&buf, "  - %s: %s\n", k, data.Annotations.SessionMetadata[k])
		}
		buf.WriteString("\n")
	}

	for _, s := range data.States {
		fmt.Fprintf(&buf, "Step %d: %s\n", s.Step, s.Summary)
		fmt.Fprintf(&buf, "  Operation: %s\n", s.Operation)
		if s.EventType != "" {
			fmt.Fprintf(&buf, "  Event:     %s\n", s.EventType)
		}
		if s.Contract != "" {
			fmt.Fprintf(&buf, "  Contract:  %s\n", s.Contract)
		}
		if s.Function != "" {
			fmt.Fprintf(&buf, "  Function:  %s\n", s.Function)
		}
		if s.SourceFile != "" {
			fmt.Fprintf(&buf, "  Source:    %s:%d\n", s.SourceFile, s.SourceLine)
		}
		if s.GitHubLink != "" {
			fmt.Fprintf(&buf, "  GitHub:    %s\n", s.GitHubLink)
		}
		fmt.Fprintf(&buf, "  Arguments: %s\n", s.Args)
		if s.Return != "" && s.Return != "<nil>" {
			fmt.Fprintf(&buf, "  Return:    %s\n", s.Return)
		}
		if s.Error != "" {
			fmt.Fprintf(&buf, "  Error:     %s\n", s.Error)
		}
		if s.CostSummary != "" {
			fmt.Fprintf(&buf, "  Cost:      %s\n", s.CostSummary)
		}
		for _, line := range s.CostBreakdown {
			fmt.Fprintf(&buf, "    * %s\n", line)
		}
		for _, detail := range s.Details {
			fmt.Fprintf(&buf, "    - %s\n", detail)
		}
		buf.WriteString("\n")
	}

	return buf.String(), nil
}

func buildExportStates(trace *ExecutionTrace) []exportState {
	states := make([]exportState, 0, len(trace.States))
	for _, s := range trace.States {
		details := make([]string, 0)
		if s.Error != "" {
			details = append(details, fmt.Sprintf("error: %s", s.Error))
		}
		if s.Operation != "" {
			details = append(details, fmt.Sprintf("operation: %s", s.Operation))
		}
		if s.ContractID != "" {
			details = append(details, fmt.Sprintf("contract: %s", s.ContractID))
		}
		if s.Function != "" {
			details = append(details, fmt.Sprintf("function: %s", s.Function))
		}
		if s.WasmInstruction != "" {
			details = append(details, fmt.Sprintf("wasm instruction: %s", s.WasmInstruction))
		}
		if len(s.Arguments) > 0 {
			details = append(details, fmt.Sprintf("arguments: %v", s.Arguments))
		}
		if s.RawArguments != nil && len(s.RawArguments) > 0 {
			details = append(details, fmt.Sprintf("raw arguments: %v", s.RawArguments))
		}
		if s.HostState != nil {
			details = append(details, fmt.Sprintf("host state entries: %d", len(s.HostState)))
		}
		if s.Memory != nil {
			details = append(details, fmt.Sprintf("memory entries: %d", len(s.Memory)))
		}
		if s.Cost != nil {
			details = append(details, fmt.Sprintf("cost: %s", FormatCostAnnotation(s.Cost)))
		}

		// A nil return value must render as "absent" in every format; formatting
		// it with %v would leak a literal "<nil>" into Markdown and HTML output
		// while the text exporter suppresses it.
		returnStr := ""
		if s.ReturnValue != nil {
			returnStr = fmt.Sprintf("%v", s.ReturnValue)
		}

		summary := s.Operation
		if summary == "" {
			summary = s.EventType
		}
		if summary == "" && s.ContractID != "" {
			summary = s.ContractID
		}
		if summary == "" {
			summary = fmt.Sprintf("step %d", s.Step)
		}

		states = append(states, exportState{
			Step:             s.Step,
			Summary:          summary,
			Operation:        s.Operation,
			EventType:        s.EventType,
			Contract:         s.ContractID,
			Function:         s.Function,
			ContractMetadata: s.ContractMetadata,
			Args:             fmt.Sprintf("%v", s.Arguments),
			Return:           returnStr,
			Error:            s.Error,
			SourceFile:       s.SourceFile,
			SourceLine:       s.SourceLine,
			GitHubLink:       s.GitHubLink,
			CostSummary:      FormatCostAnnotation(s.Cost),
			CostBreakdown:    FormatCostBreakdown(s.Cost),
			Details:          details,
		})
	}
	return states
}

func mergeTraceAnnotations(base TraceAnnotations, opts ExportOptions) TraceAnnotations {
	out := base
	if len(opts.Comments) > 0 {
		out.Comments = append(append([]string(nil), base.Comments...), opts.Comments...)
	}
	if len(opts.SessionMetadata) > 0 {
		merged := make(map[string]string, len(base.SessionMetadata)+len(opts.SessionMetadata))
		for k, v := range base.SessionMetadata {
			merged[k] = v
		}
		for k, v := range opts.SessionMetadata {
			merged[k] = v
		}
		out.SessionMetadata = merged
	}
	if out.GeneratedAt.IsZero() && (len(out.Comments) > 0 || len(out.SessionMetadata) > 0) {
		out.GeneratedAt = time.Now()
	}
	return out
}
