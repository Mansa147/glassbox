// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package visualizer

import (
	"os"
	"sync/atomic"

	"github.com/dotandev/glassbox/internal/termctx"
	"github.com/mattn/go-isatty"
)

// ANSI SGR (Select Graphic Rendition) escape codes for terminal colors.
// Redundant constants removed as they are defined in ansi.go

// globalNoColor is set by SetNoColor to allow programmatic override (e.g.
// from the --no-color CLI flag) without mutating the process environment.
var globalNoColor atomic.Bool

// SetNoColor enables or disables color output globally. Calling this with true
// is equivalent to setting the NO_COLOR environment variable and also causes
// the fatih/color library to emit plain text. It is intended to be called once
// during CLI startup before any output is produced.
func SetNoColor(v bool) {
	globalNoColor.Store(v)
}

func noColorSet() bool {
	if globalNoColor.Load() {
		return true
	}
	// Standard NO_COLOR convention (https://no-color.org).
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	// Glassbox-specific env var for tooling that cannot set NO_COLOR globally.
	return os.Getenv("GLASSBOX_NO_COLOR") != ""
}

// ColorEnabled reports whether ANSI color output should be used.
func ColorEnabled() bool {
	// Non-interactive mode (--non-interactive flag, CI env, or pipe) suppresses
	// all terminal control sequences including color.
	if termctx.GlobalNonInteractive() {
		return false
	}
	// NO_COLOR / GLASSBOX_NO_COLOR / global override must always take precedence.
	if noColorSet() {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd())
}

// colorMap maps color names to ANSI SGR codes.
var colorMap = map[string]string{ //nolint:unused // Reserved for future use in dynamic color mapping
	"red":     sgrRed,
	"green":   sgrGreen,
	"yellow":  sgrYellow,
	"blue":    sgrBlue,
	"magenta": sgrMagenta,
	"cyan":    sgrCyan,
	"bold":    sgrBold,
	"dim":     sgrDim,
}

// Colorize returns text with ANSI color if enabled, otherwise plain text.
func Colorize(text string, color string) string {
	if !ColorEnabled() {
		return text
	}

	var code string
	switch color {
	case "red":
		code = sgrRed
	case "green":
		code = sgrGreen
	case "yellow":
		code = sgrYellow
	case "blue":
		code = sgrBlue
	case "magenta":
		code = sgrMagenta
	case "cyan":
		code = sgrCyan
	case "dim":
		code = sgrDim
	case "bold":
		code = sgrBold
	default:
		return text
	}

	return code + text + sgrReset
}

// ContractBoundary returns a visual separator for cross-contract call transitions.
func ContractBoundary(fromContract, toContract string) string {

	line := "--- contract boundary: " + fromContract + " -> " + toContract + " ---"
	if !ColorEnabled() {
		return line
	}
	return sgrMagenta + sgrBold + line + sgrReset
}

// Success returns a success indicator.
func Success() string {
	return Colorize("[OK]", "green")
}

// Warning returns a warning indicator.
func Warning() string {
	return Colorize("[!]", "yellow")
}

// Error returns an error indicator.
func Error() string {
	return Colorize("[FAIL]", "red")
}

// Info returns an info indicator.
func Info() string {
	return Colorize("[i]", "cyan")
}

// Symbol returns a symbol name rendered as ASCII markers.
//
//nolint:gocyclo // Large switch statement mapping symbol names to ASCII representations
func Symbol(name string) string {
	if ColorEnabled() {
		switch name {
		case "check":
			return "[OK]"
		case "cross":
			return "[FAIL]"
		case "warn":
			return "[!]"
		case "arrow_r":
			return "->"
		case "arrow_l":
			return "<-"
		case "target":
			return "[TARGET]"
		case "pin":
			return "*"
		case "wrench":
			return "[TOOL]"
		case "chart":
			return "[STATS]"
		case "list":
			return "[LIST]"
		case "play":
			return "[PLAY]"
		case "book":
			return "[DOC]"
		case "wave":
			return "[HELLO]"
		case "magnify":
			return "[SEARCH]"
		case "logs":
			return "[LOGS]"
		case "events":
			return "[NET]"
		default:
			return name
		}
	}

	switch name {
	case "check":
		return "[OK]"
	case "cross":
		return "[X]"
	case "warn":
		return "[!]"
	case "arrow_r":
		return "->"
	case "arrow_l":
		return "<-"
	case "target":
		return ">>"
	case "pin":
		return "*"
	case "wrench":
		return "[*]"
	case "chart":
		return "[#]"
	case "list":
		return "[.]"
	case "play":
		return ">"
	case "book":
		return "[?]"
	case "wave":
		return ""
	case "magnify":
		return "[?]"
	case "logs":
		return "[Logs]"
	case "events":
		return "[Events]"
	default:
		return name
	}
}
