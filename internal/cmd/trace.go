// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dotandev/glassbox/internal/decoder"
	"github.com/dotandev/glassbox/internal/errors"
	"github.com/dotandev/glassbox/internal/gasmodel"
	"github.com/dotandev/glassbox/internal/trace"
	"github.com/dotandev/glassbox/internal/visualizer"
	"github.com/spf13/cobra"
)

var (
	traceFile            string
	traceThemeFlag       string
	tracePrint           bool
	traceNoColor         bool
	traceExportSVG       string
	traceOutputJSON      string
	traceExportPath      string
	traceExportFormat    string
	traceExportMarkdown  string
	traceAnnotationsFlag string
	traceGasModelPath    string
	traceComments        []string
	traceMetadata        []string
	traceVerbosity       string
	traceDryRunFlag      bool
	traceShowTimingFlag  bool
	traceForceFlag       bool
	traceFormatAlias     string // --format is a user-friendly alias for --export-format

	// eventSchemas is optionally populated by other subsystems (e.g. schema
	// loading) before the trace command runs. Nil is safe — PrintExecutionTrace
	// handles the absence gracefully.
	eventSchemas *trace.EventSchemaSet
)

// validTraceExportFormats lists the formats accepted by --export-format / --format.
var validTraceExportFormats = map[string]bool{
	"html":     true,
	"markdown": true,
	"md":       true,
	"json":     true,
	"text":     true,
}

var traceCmd = &cobra.Command{
	Use:     "trace <trace-file>",
	GroupID: "core",
	Short:   "Interactive trace navigation and debugging",
	Long: `Launch an interactive trace viewer for bi-directional navigation through execution traces.

The trace viewer allows you to:
- Step forward and backward through execution
- Jump to specific steps
- Reconstruct state at any point
- View memory and host state changes

Use --print for a one-shot, colour-coded ASCII tree report suitable for CI
logs or piping to other tools. Add --no-color to disable ANSI colours.

Export formats (--export / --format):
  html      — interactive HTML page, best for manual analysis in a browser
  markdown  — shareable Markdown report, best for issues and chat
  json      — machine-readable JSON, best for CI/CD and automated processing
  text      — plain text, best for simple logging or piping

Performance notes:
  Large traces (>5 000 steps) can produce slow HTML rendering.
  Use --format json for large traces or CI pipelines.
  Use --trace-verbosity summary to reduce output size significantly.
  Use --dry-run to validate parameters without writing any files.`,
	Example: `  # Open the interactive trace viewer
  glassbox trace execution.json

  # Validate parameters without writing any output (dry-run)
  glassbox trace --dry-run --export trace.html execution.json

  # Export trace as interactive HTML
  glassbox trace --export trace.html --format html execution.json

  # Export as JSON (best for large traces and CI pipelines)
  glassbox trace --export trace.json --format json execution.json

  # Export as markdown for sharing in chat or issue trackers
  glassbox trace --export trace.md --format markdown execution.json

  # Export as plain text
  glassbox trace --export trace.txt --format text execution.json

  # Print a colour-coded ASCII tree and exit (suitable for CI logs)
  glassbox trace --print execution.json

  # Print with timing info
  glassbox trace --print --show-timing execution.json

  # Export call graph as SVG
  glassbox trace --export-svg callgraph.svg execution.json

  # Export with comments and session metadata
  glassbox trace --export report.md --format markdown \
    --comment "Reviewed with Alice" --meta env=testnet execution.json

  # Force overwrite of an existing output file
  glassbox trace --export trace.html --force execution.json`,
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		var failures []string

		// --format is an alias for --export-format; merge the two.
		// If the user typed --format instead of --export-format, honour it.
		if cmd.Flags().Changed("format") && !cmd.Flags().Changed("export-format") {
			traceExportFormat = traceFormatAlias
		}

		// --dry-run: validate but don't write anything.
		// It implies an export target must be specified (otherwise nothing to validate).
		if traceDryRunFlag {
			if traceExportPath == "" && traceExportMarkdown == "" && traceOutputJSON == "" && traceExportSVG == "" {
				failures = append(failures, "--dry-run requires an export target (--export, --output-json, or --export-svg)\n"+
					"  Fix: add an export flag, e.g. --export trace.html --format html")
			}
		}

		// Validate --export-format (and its --format alias) when --export is set.
		if traceExportPath != "" {
			normalised := strings.ToLower(strings.TrimSpace(traceExportFormat))
			if !validTraceExportFormats[normalised] {
				failures = append(failures, fmt.Sprintf(
					"invalid --format %q — must be one of: html, markdown, json, text\n"+
						"  Fix: use --format html (interactive), markdown (shareable), json (machine-readable), or text (plain)",
					traceExportFormat,
				))
			}
			// Validate path safety: null bytes, symlink resolution, existing-directory guard.
			if _, err := ValidateOutputPath("export", traceExportPath); err != nil {
				failures = append(failures, err.Error())
			}
		}

		// Validate --export-markdown path (deprecated alias).
		if traceExportMarkdown != "" {
			if _, err := ValidateOutputPath("export-markdown", traceExportMarkdown); err != nil {
				failures = append(failures, err.Error())
			}
		}

		// Validate --output-json path.
		if traceOutputJSON != "" {
			if _, err := ValidateOutputPath("output-json", traceOutputJSON); err != nil {
				failures = append(failures, err.Error())
			}
		}

		// Validate --export-svg path.
		if traceExportSVG != "" {
			if _, err := ValidateOutputPath("export-svg", traceExportSVG); err != nil {
				failures = append(failures, err.Error())
			}
		}

		// --export and --print are mutually exclusive.
		if traceExportPath != "" && tracePrint {
			failures = append(failures, "cannot specify both --export and --print\n"+
				"  Fix: use --export to write to a file, or --print to output to stdout — not both")
		}

		// --export-markdown and --export are mutually exclusive.
		if traceExportMarkdown != "" && traceExportPath != "" {
			failures = append(failures, "cannot specify both --export-markdown and --export\n"+
				"  Fix: use --export --format markdown for the same result as --export-markdown")
		}

		// Validate --trace-verbosity when set.
		if traceVerbosity != "" {
			if _, err := trace.ParseVerbosity(traceVerbosity); err != nil {
				failures = append(failures, fmt.Sprintf(
					"invalid --trace-verbosity %q — must be one of: summary, normal, verbose\n"+
						"  Fix: use --trace-verbosity normal (default), summary (minimal), or verbose (detailed)",
					traceVerbosity,
				))
			}
		}

		// Validate --annotations file exists when set.
		// NOTE: annotation loading (LoadAnnotationFile / BuildTraceNodeTree)
		// is not yet implemented. The flag is accepted and validated for path
		// correctness so the UI is consistent, but the overlay is skipped at
		// runtime with a warning until the helper functions are available.
		if traceAnnotationsFlag != "" {
			if _, err := ValidateInputPath("annotations", traceAnnotationsFlag); err != nil {
				failures = append(failures, err.Error())
			}
		}

		// Validate --gas-model file exists when set.
		if traceGasModelPath != "" {
			if _, err := ValidateInputPath("gas-model", traceGasModelPath); err != nil {
				failures = append(failures, err.Error())
			}
		}

		// Validate --meta values are in key=value format with a non-empty key.
		// This pre-flight check surfaces malformed flags before the trace file
		// is loaded so users fix all CLI issues in a single pass.
		for _, entry := range traceMetadata {
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
				failures = append(failures, fmt.Sprintf(
					"--meta value %q is not in key=value format\n"+
						"  Fix: supply metadata as key=value pairs, e.g. --meta env=testnet --meta version=1.2",
					entry,
				))
			}
		}

		// Validate --comment values are not empty or whitespace-only.
		for i, comment := range traceComments {
			if strings.TrimSpace(comment) == "" {
				failures = append(failures, fmt.Sprintf(
					"--comment value at position %d is empty or whitespace-only\n"+
						"  Fix: provide non-empty comment text or omit the empty --comment flag",
					i,
				))
			}
		}

		if len(failures) == 1 {
			return errors.WrapValidationError(failures[0])
		}
		if len(failures) > 1 {
			lines := make([]string, 0, len(failures)+1)
			lines = append(lines, fmt.Sprintf("%d trace command validation error(s):", len(failures)))
			for i, f := range failures {
				lines = append(lines, fmt.Sprintf("  %d. %s", i+1, f))
			}
			return errors.WrapValidationError(strings.Join(lines, "\n"))
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Apply theme if specified, otherwise auto-detect.
		if traceThemeFlag != "" {
			visualizer.SetTheme(visualizer.Theme(traceThemeFlag))
		} else {
			visualizer.SetTheme(visualizer.DetectTheme())
		}

		// Resolve the trace file path (positional arg or --file flag).
		var filename string
		if len(args) > 0 {
			filename = args[0]
		} else if traceFile != "" {
			filename = traceFile
		} else {
			return errors.WrapValidationError(
				"trace file is required\n" +
					"  Usage: glassbox trace <trace-file>\n" +
					"  Or:    glassbox trace --file <trace-file>\n" +
					"  Run 'glassbox trace --help' for all available options")
		}

		// Validate the trace file path — normalizes, resolves symlinks, checks
		// existence and readability using the security-aware path validator.
		normalizedFilename, pathErr := ValidateInputPath("file", filename)
		if pathErr != nil {
			// Produce a user-friendly error that includes the original value.
			return errors.WrapValidationError(fmt.Sprintf(
				"trace file not found or not readable: %q\n"+
					"  Fix: verify the path is correct and the file exists\n"+
					"  Tip: trace files are produced by 'glassbox debug --trace-output <file>'",
				filename,
			))
		}
		filename = normalizedFilename

		var loadStart time.Time
		if traceShowTimingFlag {
			loadStart = time.Now()
		}

		data, err := os.ReadFile(filename)
		if err != nil {
			return errors.WrapValidationError(fmt.Sprintf(
				"failed to read trace file %q: %v\n"+
					"  Fix: ensure you have read permissions for the file",
				filename, err,
			))
		}

		executionTrace, err := trace.FromJSON(data)
		if err != nil {
			return errors.WrapUnmarshalFailed(err,
				fmt.Sprintf(
					"failed to parse trace file %q — the file may be corrupted or not a valid Glassbox trace\n"+
						"  Fix: verify the file is valid JSON with 'jq . %q'\n"+
						"  Tip: re-export the trace with 'glassbox debug --trace-output <file>'",
					filename, filename,
				))
		}

		if traceShowTimingFlag {
			fmt.Fprintf(cmd.ErrOrStderr(), "  load:   %s (%d steps, %d bytes)\n",
				time.Since(loadStart).Round(time.Millisecond), len(executionTrace.States), len(data))
		}

		// Emit size/performance warnings for large traces via the compatibility
		// validator so users know before the export starts.
		if traceExportPath != "" || traceExportMarkdown != "" {
			targetFmt := traceExportFormat
			if traceExportMarkdown != "" {
				targetFmt = "markdown"
			}
			compatWarnings := trace.ValidateFormatCompatibility(executionTrace, targetFmt, trace.DefaultCompatibilityOptions())
			for _, w := range compatWarnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", w)
			}
		}

		// Annotate with gas model estimates if requested.
		if traceGasModelPath != "" {
			model, err := gasmodel.ParseGasModel(traceGasModelPath)
			if err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"failed to load gas model from %q: %v\n"+
						"  Fix: verify the gas model JSON file is valid",
					traceGasModelPath, err,
				))
			}
			trace.AnnotateExecutionCosts(executionTrace, nil, model)
		}

		// Merge any --comment / --meta annotations supplied on the command line.
		if len(traceComments) > 0 || len(traceMetadata) > 0 {
			opts, err := traceExportOptions()
			if err != nil {
				return err
			}
			executionTrace.Annotations.Comments = append(executionTrace.Annotations.Comments, opts.Comments...)
			if executionTrace.Annotations.SessionMetadata == nil {
				executionTrace.Annotations.SessionMetadata = make(map[string]string)
			}
			for k, v := range opts.SessionMetadata {
				executionTrace.Annotations.SessionMetadata[k] = v
			}
		}

		// --dry-run: validate parameters and compatibility but write nothing.
		if traceDryRunFlag {
			issues := trace.ValidateExecutionTrace(executionTrace)
			if len(issues) > 0 {
				for _, issue := range issues {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", issue)
				}
			}
			targetFmt := traceExportFormat
			if traceExportMarkdown != "" {
				targetFmt = "markdown"
			}
			if err := trace.ValidateTraceExportParams(executionTrace, targetFmt, traceExportPath, trace.ExportOptions{}); err != nil {
				return errors.WrapValidationError(fmt.Sprintf("dry-run validation failed: %v", err))
			}
			fmt.Printf("%s Dry-run complete — %d step(s), format %q validated. No files written.\n",
				visualizer.Symbol("success"), len(executionTrace.States), targetFmt)
			return nil
		}

		// --output-json: write deterministic schema'd JSON export and exit.
		if traceOutputJSON != "" {
			if !traceForceFlag {
				if _, statErr := os.Stat(traceOutputJSON); statErr == nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %q already exists and will be overwritten. Use --force to suppress this warning.\n", traceOutputJSON)
				}
			}
			var jsonStart time.Time
			if traceShowTimingFlag {
				jsonStart = time.Now()
			}
			jsonData, err := executionTrace.ExportJSON(trace.CurrentJSONSchemaVersion, time.Now())
			if err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"failed to serialize trace as JSON: %v\n"+
						"  This may indicate the trace contains non-serializable data",
					err,
				))
			}
			if err := os.WriteFile(traceOutputJSON, jsonData, 0o644); err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"failed to write JSON export to %q: %v\n"+
						"  Fix: ensure you have write permissions and sufficient disk space",
					traceOutputJSON, err,
				))
			}
			sizeStr := humanFileSize(int64(len(jsonData)))
			if traceShowTimingFlag {
				fmt.Fprintf(cmd.ErrOrStderr(), "  export: %s\n", time.Since(jsonStart).Round(time.Millisecond))
			}
			fmt.Printf("%s Trace exported to: %s (%s)\n", visualizer.Symbol("success"), traceOutputJSON, sizeStr)
			return nil
		}

		// Apply verbosity filter before rendering.
		verbosityLevel, err := trace.ParseVerbosity(traceVerbosity)
		if err != nil {
			return errors.WrapValidationError(fmt.Sprintf(
				"invalid --trace-verbosity %q — must be one of: summary, normal, verbose\n"+
					"  Fix: use --trace-verbosity normal (default), summary (minimal), or verbose (detailed)",
				traceVerbosity,
			))
		}
		executionTrace = trace.FilterExecutionTrace(executionTrace, verbosityLevel)

		// --print: render a rich ASCII tree report then exit (non-interactive).
		if tracePrint {
			if traceAnnotationsFlag != "" {
				// Annotation overlay for printed tree output requires LoadAnnotationFile
				// and BuildTraceNodeTree — these are registered on the --annotations
				// flag but the underlying helpers are not yet available in this build.
				// Warn the user rather than silently ignoring the flag.
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Warning: --annotations is registered but the annotation loader is not yet implemented in this build; the flag will be ignored.\n"+
						"  The trace will be printed without annotation overlay.\n",
				)
			}
			var printStart time.Time
			if traceShowTimingFlag {
				printStart = time.Now()
			}
			opts := trace.PrintOptions{
				NoColor:      traceNoColor || NoColorFlag,
				EventSchemas: eventSchemas,
			}
			trace.PrintExecutionTrace(executionTrace, opts)
			if traceShowTimingFlag {
				fmt.Fprintf(cmd.ErrOrStderr(), "  render: %s\n", time.Since(printStart).Round(time.Millisecond))
			}
			return nil
		}

		// --export-markdown: deprecated alias — emit a deprecation notice.
		if traceExportMarkdown != "" {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"Deprecated: --export-markdown is deprecated; use --export %s --format markdown instead.\n",
				traceExportMarkdown,
			)
			if !traceForceFlag {
				if _, statErr := os.Stat(traceExportMarkdown); statErr == nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %q already exists and will be overwritten. Use --force to suppress this warning.\n", traceExportMarkdown)
				}
			}
			var mdStart time.Time
			if traceShowTimingFlag {
				fmt.Fprintf(cmd.ErrOrStderr(), "Exporting %d steps as markdown...\n", len(executionTrace.States))
				mdStart = time.Now()
			}
			if err := trace.ExportExecutionTrace(executionTrace, "markdown", traceExportMarkdown); err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"failed to export trace as Markdown to %q: %v\n"+
						"  Fix: ensure the output directory exists and you have write permissions",
					traceExportMarkdown, err,
				))
			}
			sizeStr := traceExportedFileSize(traceExportMarkdown)
			if traceShowTimingFlag {
				fmt.Fprintf(cmd.ErrOrStderr(), "  export: %s\n", time.Since(mdStart).Round(time.Millisecond))
			}
			fmt.Printf("%s Trace exported to: %s%s\n", visualizer.Symbol("success"), traceExportMarkdown, sizeStr)
			return nil
		}

		// --export-svg: generate a call graph SVG and exit.
		if traceExportSVG != "" {
			if len(executionTrace.DiagnosticEvents) == 0 {
				return errors.WrapValidationError(
					"no diagnostic events found in trace — call graph cannot be generated\n" +
						"  Possible causes:\n" +
						"    - The trace was captured without diagnostic events\n" +
						"    - The transaction did not call any contracts\n" +
						"  Fix: re-run with a transaction that includes contract calls\n" +
						"  Tip: use --trace-verbosity verbose when capturing the trace for maximum detail",
				)
			}
			if !traceForceFlag {
				if _, statErr := os.Stat(traceExportSVG); statErr == nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %q already exists and will be overwritten. Use --force to suppress this warning.\n", traceExportSVG)
				}
			}

			maxDepth := 50
			callTree, err := decoder.DecodeDiagnosticEvents(executionTrace.DiagnosticEvents, maxDepth)
			if err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"failed to decode call tree from diagnostic events: %v\n"+
						"  Fix: verify the trace file is complete and not corrupted",
					err,
				))
			}
			svg := visualizer.GenerateCallGraphSVG(callTree, maxDepth)
			if err := os.WriteFile(traceExportSVG, []byte(svg), 0o644); err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"failed to write SVG to %q: %v\n"+
						"  Fix: ensure the output directory exists and you have write permissions",
					traceExportSVG, err,
				))
			}
			sizeStr := humanFileSize(int64(len(svg)))
			fmt.Printf("%s Call graph exported to: %s (%s)\n", visualizer.Symbol("success"), traceExportSVG, sizeStr)
			return nil
		}

		// --export: write a trace report in the specified format and exit.
		if traceExportPath != "" {
			if !traceForceFlag {
				if _, statErr := os.Stat(traceExportPath); statErr == nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %q already exists and will be overwritten. Use --force to suppress this warning.\n", traceExportPath)
				}
			}

			opts, err := traceExportOptions()
			if err != nil {
				return err
			}

			// Emit a pre-export progress line so large exports don't appear to hang.
			fmt.Fprintf(cmd.ErrOrStderr(), "Exporting %d steps as %s...\n", len(executionTrace.States), traceExportFormat)
			var exportStart time.Time
			if traceShowTimingFlag {
				exportStart = time.Now()
			}

			// Route through ExportWithCompatibility so size warnings and version
			// information are correctly applied (bridges the gap between the
			// lower-level ExportExecutionTraceWithOptions and the compatibility layer).
			if err := trace.ExportWithCompatibility(executionTrace, traceExportFormat, traceExportPath, opts, trace.DefaultCompatibilityOptions()); err != nil {
				return errors.WrapValidationError(fmt.Sprintf(
					"failed to export trace as %s to %q: %v\n"+
						"  Fix: ensure the output directory exists and you have write permissions",
					traceExportFormat, traceExportPath, err,
				))
			}

			sizeStr := traceExportedFileSize(traceExportPath)
			if traceShowTimingFlag {
				fmt.Fprintf(cmd.ErrOrStderr(), "  export: %s\n", time.Since(exportStart).Round(time.Millisecond))
			}
			fmt.Printf("%s Trace exported to: %s%s\n", visualizer.Symbol("success"), traceExportPath, sizeStr)
			return nil
		}

		// Default: start the interactive viewer.
		viewer := trace.NewInteractiveViewer(executionTrace)
		return viewer.Start()
	},
}

func init() {
	traceCmd.Flags().StringVarP(&traceFile, "file", "f", "", "Trace file to load")
	traceCmd.Flags().StringVar(&traceThemeFlag, "theme", "", "Color theme (default, dark, light, deuteranopia, protanopia, tritanopia, high-contrast)")
	traceCmd.Flags().BoolVar(&tracePrint, "print", false, "Print a rich ASCII tree report and exit (non-interactive)")
	traceCmd.Flags().BoolVar(&traceNoColor, "no-color", false, "Disable ANSI colour output (also honoured via NO_COLOR env var)")
	traceCmd.Flags().StringVar(&traceExportSVG, "export-svg", "", "Export call graph as SVG to specified file")
	traceCmd.Flags().StringVar(&traceOutputJSON, "output-json", "", "Export trace as deterministic JSON to specified file (includes schema_version)")
	traceCmd.Flags().StringVar(&traceExportPath, "export", "", "Export trace report to a file (use with --format)")
	traceCmd.Flags().StringVar(&traceExportFormat, "export-format", "html", "Trace export format: html, markdown, json, or text (use --format as an alias)")
	traceCmd.Flags().StringVar(&traceFormatAlias, "format", "", "Export format alias for --export-format: html, markdown, json, or text")
	traceCmd.Flags().StringVar(&traceExportMarkdown, "export-markdown", "", "Export trace as Markdown to specified file (deprecated: use --export --format markdown)")
	traceCmd.Flags().StringVar(&traceAnnotationsFlag, "annotations", "", "Path to a JSON file containing step annotations to overlay on the trace")
	traceCmd.Flags().StringVar(&traceGasModelPath, "gas-model", "", "Gas model JSON used to annotate contract call cost estimates")
	traceCmd.Flags().StringVar(&traceVerbosity, "trace-verbosity", "normal", "Trace detail level: summary, normal, or verbose")
	traceCmd.Flags().StringArrayVar(&traceComments, "comment", nil, "Comment to include in exported trace artifacts; repeatable")
	traceCmd.Flags().StringArrayVar(&traceMetadata, "meta", nil, "Session metadata for exported trace artifacts in key=value form; repeatable")
	traceCmd.Flags().BoolVar(&traceDryRunFlag, "dry-run", false, "Validate parameters and trace data without writing any files")
	traceCmd.Flags().BoolVar(&traceForceFlag, "force", false, "Overwrite existing output files without prompting")
	traceCmd.Flags().BoolVar(&traceShowTimingFlag, "show-timing", false, "Print load, render, and export timing to stderr")

	_ = traceCmd.RegisterFlagCompletionFunc("theme", completeThemeFlag)
	_ = traceCmd.RegisterFlagCompletionFunc("export-format", completeTraceExportFormatFlag)
	_ = traceCmd.RegisterFlagCompletionFunc("format", completeTraceExportFormatFlag)
	_ = traceCmd.RegisterFlagCompletionFunc("trace-verbosity", completeTraceVerbosityFlag)

	rootCmd.AddCommand(traceCmd)
}

func traceExportOptions() (trace.ExportOptions, error) {
	metadata := make(map[string]string, len(traceMetadata))
	for _, entry := range traceMetadata {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return trace.ExportOptions{}, errors.WrapValidationError(
				fmt.Sprintf(
					"--meta value %q is not in key=value format\n"+
						"  Fix: supply metadata as key=value pairs, e.g. --meta env=testnet --meta version=1.2",
					entry,
				),
			)
		}
		metadata[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return trace.ExportOptions{
		Comments:        traceComments,
		SessionMetadata: metadata,
	}, nil
}
