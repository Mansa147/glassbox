// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package dwarf

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// DWARF version range supported by Go's debug/dwarf reader, which is what
// glassbox relies on for source mapping. Versions outside this range parse
// partially or not at all, and the difference must be reported precisely
// rather than surfaced as a generic "no debug info" error.
const (
	// MinSupportedDWARFVersion is the oldest DWARF version glassbox can map.
	MinSupportedDWARFVersion = 2
	// MaxSupportedDWARFVersion is the newest DWARF version glassbox can map.
	MaxSupportedDWARFVersion = 5
)

// WarningKind classifies why a binary cannot be fully mapped. Callers can
// switch on the kind to present tailored guidance instead of parsing the
// human-readable message.
type WarningKind string

const (
	// WarnMissingDebugSections indicates the binary was stripped of (or never
	// compiled with) the DWARF sections required for source mapping.
	WarnMissingDebugSections WarningKind = "missing_debug_sections"
	// WarnUnsupportedVersion indicates a DWARF version outside the range that
	// glassbox's parser understands.
	WarnUnsupportedVersion WarningKind = "unsupported_dwarf_version"
	// WarnPartialDebugInfo indicates that some debug sections are present but
	// others required for full mapping (variables, functions) are missing.
	WarnPartialDebugInfo WarningKind = "partial_debug_info"
)

// Warning is a structured diagnostic describing a mapping limitation together
// with concrete compiler/build guidance for resolving it.
type Warning struct {
	// Kind classifies the limitation for programmatic handling.
	Kind WarningKind
	// Message is a human-readable description of the limitation.
	Message string
	// Hint is a build/compiler command or setting that resolves the
	// limitation. It is empty when no user action would help.
	Hint string
}

// String renders the warning as "message (hint: ...)" for logging.
func (w Warning) String() string {
	if w.Hint == "" {
		return w.Message
	}
	return fmt.Sprintf("%s (hint: %s)", w.Message, w.Hint)
}

// MappingSupport reports which classes of source mapping are available for a
// binary given the debug sections it actually contains.
type MappingSupport struct {
	// SourceLines is true when address→file:line mapping is possible
	// (requires .debug_line).
	SourceLines bool
	// LocalVars is true when local-variable extraction is possible
	// (requires .debug_info and .debug_abbrev).
	LocalVars bool
	// InlineFrames is true when inlined-frame reconstruction is possible
	// (requires .debug_info).
	InlineFrames bool
}

// Capabilities is the result of inspecting a binary's DWARF debug information
// before parsing. It distinguishes a supported file, an unsupported DWARF
// version, and missing debug sections so that callers can report the precise
// reason a mapping is unavailable.
type Capabilities struct {
	// BinaryType is the detected container format: "wasm", "elf", "macho",
	// "pe", or "" when the format is unrecognised.
	BinaryType string
	// Version is the DWARF version read from .debug_info, or 0 when no
	// .debug_info section is present.
	Version int
	// Sections maps present DWARF section names (e.g. ".debug_info") to their
	// size in bytes.
	Sections map[string]int
	// Supported is true when glassbox can produce at least a partial source
	// mapping from this binary.
	Supported bool
	// Mappings reports which specific mapping capabilities are available.
	Mappings MappingSupport
	// Warnings lists structured limitations with remediation hints. It is
	// empty when the binary is fully supported.
	Warnings []Warning
}

// HasSection reports whether the named DWARF section is present.
func (c *Capabilities) HasSection(name string) bool {
	_, ok := c.Sections[name]
	return ok
}

// Summary returns a single-line human-readable description of the binary's
// DWARF capabilities, suitable for logs or CLI output.
func (c *Capabilities) Summary() string {
	if c.BinaryType == "" {
		return "unrecognised binary format: no DWARF capability information available"
	}
	if len(c.Sections) == 0 {
		return fmt.Sprintf("%s binary: no DWARF debug sections present", c.BinaryType)
	}
	ver := "unknown"
	if c.Version > 0 {
		ver = fmt.Sprintf("v%d", c.Version)
	}
	names := make([]string, 0, len(c.Sections))
	for n := range c.Sections {
		names = append(names, n)
	}
	sort.Strings(names)
	return fmt.Sprintf(
		"%s binary: DWARF %s, sections [%s], supported=%t",
		c.BinaryType, ver, strings.Join(names, " "), c.Supported,
	)
}

// buildCommandHint is the canonical remediation for a WASM contract lacking
// debug info. It is reused across warnings so the guidance stays consistent.
const buildCommandHint = "recompile with debug = true under [profile.release] in Cargo.toml, " +
	"then: cargo build --release --target wasm32-unknown-unknown"

// DetectCapabilities inspects a binary and reports its DWARF version, the debug
// sections it contains, and which source-mapping capabilities are available.
// It never parses the DWARF tree, so it is cheap and cannot fail on malformed
// debug data — it only reads the section table and the compilation-unit header.
//
// The returned Capabilities always distinguishes three cases the caller cares
// about:
//
//   - missing debug sections  → Warnings contains WarnMissingDebugSections with
//     a build-command hint.
//   - unsupported DWARF version → Warnings contains WarnUnsupportedVersion
//     naming the detected version and the supported range.
//   - partial debug info → Warnings contains WarnPartialDebugInfo describing
//     which capabilities are unavailable.
//
// A fully supported binary returns Supported=true with no warnings.
func DetectCapabilities(data []byte) *Capabilities {
	caps := &Capabilities{Sections: map[string]int{}}

	sections, binaryType, order := extractDebugSections(data)
	caps.BinaryType = binaryType

	if binaryType == "" {
		caps.Warnings = append(caps.Warnings, Warning{
			Kind:    WarnMissingDebugSections,
			Message: "unrecognised binary format: not a WASM, ELF, Mach-O, or PE file",
		})
		return caps
	}

	for name, content := range sections {
		caps.Sections[name] = len(content)
	}

	// No DWARF sections at all → the binary was stripped or built without
	// debug info. This is the "stripped" fixture case.
	if len(caps.Sections) == 0 {
		caps.Warnings = append(caps.Warnings, Warning{
			Kind: WarnMissingDebugSections,
			Message: fmt.Sprintf(
				"%s binary contains no DWARF debug sections; source mapping is unavailable",
				binaryType,
			),
			Hint: buildCommandHint,
		})
		return caps
	}

	// Read the DWARF version from the .debug_info compilation-unit header when
	// present. Version 0 means .debug_info is absent (e.g. line tables only).
	if info := sections[".debug_info"]; len(info) > 0 {
		caps.Version = readDWARFVersion(info, order)
	}

	// Determine which mapping capabilities the present sections can support.
	_, hasInfo := sections[".debug_info"]
	_, hasAbbrev := sections[".debug_abbrev"]
	_, hasLine := sections[".debug_line"]

	versionOK := caps.Version == 0 ||
		(caps.Version >= MinSupportedDWARFVersion && caps.Version <= MaxSupportedDWARFVersion)

	caps.Mappings = MappingSupport{
		SourceLines:  hasLine && versionOK,
		LocalVars:    hasInfo && hasAbbrev && versionOK,
		InlineFrames: hasInfo && versionOK,
	}

	// Unsupported DWARF version: .debug_info exists but its version is outside
	// the range Go's reader understands. Name the version and the limitation.
	if caps.Version > 0 && !versionOK {
		limitation := "older than the supported range"
		if caps.Version > MaxSupportedDWARFVersion {
			limitation = "newer than the supported range"
		}
		caps.Warnings = append(caps.Warnings, Warning{
			Kind: WarnUnsupportedVersion,
			Message: fmt.Sprintf(
				"DWARF version %d is %s (glassbox supports v%d–v%d); "+
					"source mapping from .debug_info is unavailable for this file",
				caps.Version, limitation, MinSupportedDWARFVersion, MaxSupportedDWARFVersion,
			),
			Hint: "rebuild with a toolchain that emits DWARF v" +
				fmt.Sprintf("%d–v%d", MinSupportedDWARFVersion, MaxSupportedDWARFVersion) +
				" (for Rust/LLVM, pass -Zdwarf-version=5 or use the default stable emitter)",
		})
		caps.Supported = caps.Mappings.SourceLines
		return caps
	}

	// Partial debug info: some sections present, but not the full set needed
	// for variable/function mapping. Distinguish this from missing symbols —
	// the sections that ARE present will still map.
	var missing []string
	if !hasInfo {
		missing = append(missing, ".debug_info")
	}
	if !hasAbbrev {
		missing = append(missing, ".debug_abbrev")
	}
	if !hasLine {
		missing = append(missing, ".debug_line")
	}
	if len(missing) > 0 {
		var lost []string
		if !caps.Mappings.LocalVars {
			lost = append(lost, "local-variable extraction")
		}
		if !caps.Mappings.InlineFrames {
			lost = append(lost, "inlined-frame reconstruction")
		}
		if !caps.Mappings.SourceLines {
			lost = append(lost, "line-number mapping")
		}
		caps.Warnings = append(caps.Warnings, Warning{
			Kind: WarnPartialDebugInfo,
			Message: fmt.Sprintf(
				"partial DWARF debug info: missing %s; unavailable: %s",
				strings.Join(missing, ", "), strings.Join(lost, ", "),
			),
			Hint: buildCommandHint,
		})
	}

	// Supported when at least one mapping capability is available.
	caps.Supported = caps.Mappings.SourceLines || caps.Mappings.LocalVars || caps.Mappings.InlineFrames
	return caps
}

// readDWARFVersion extracts the DWARF version from the start of a .debug_info
// section. The compilation-unit header begins with a length field (4 bytes for
// 32-bit DWARF, or the 0xffffffff escape followed by an 8-byte length for
// 64-bit DWARF) immediately followed by a 2-byte version. Returns 0 when the
// section is too short to contain a version.
func readDWARFVersion(info []byte, order binary.ByteOrder) int {
	// 32-bit unit: [length:4][version:2]
	if len(info) < 6 {
		return 0
	}
	initial := order.Uint32(info[0:4])
	if initial == 0xffffffff {
		// 64-bit unit: [0xffffffff:4][length:8][version:2]
		if len(info) < 14 {
			return 0
		}
		return int(order.Uint16(info[12:14]))
	}
	return int(order.Uint16(info[4:6]))
}

// extractDebugSections detects the container format of a binary and returns its
// DWARF (.debug_*) sections keyed by canonical section name, along with the
// format identifier and the byte order used by the container. Unrecognised or
// truncated inputs yield an empty map and an empty binary type.
func extractDebugSections(data []byte) (map[string][]byte, string, binary.ByteOrder) {
	out := map[string][]byte{}
	if len(data) < 4 {
		return out, "", binary.LittleEndian
	}

	switch {
	case data[0] == 0x00 && data[1] == 0x61 && data[2] == 0x73 && data[3] == 0x6d:
		// WASM: DWARF lives in custom sections and is little-endian.
		for name, content := range parseWASMSections(data) {
			if strings.HasPrefix(name, ".debug") {
				out[name] = content
			}
		}
		return out, "wasm", binary.LittleEndian

	case data[0] == 0x7f && data[1] == 0x45 && data[2] == 0x4c && data[3] == 0x46:
		f, err := elf.NewFile(bytesToReader(data))
		if err != nil {
			return out, "elf", binary.LittleEndian
		}
		for _, s := range f.Sections {
			if strings.HasPrefix(s.Name, ".debug") || strings.HasPrefix(s.Name, ".zdebug") {
				if d, err := s.Data(); err == nil {
					out[normaliseDebugName(s.Name)] = d
				}
			}
		}
		return out, "elf", f.ByteOrder

	case len(data) >= 4 && (binary.BigEndian.Uint32(data[0:4]) == 0xfeedfacf ||
		binary.LittleEndian.Uint32(data[0:4]) == 0xfeedfacf):
		f, err := macho.NewFile(bytesToReader(data))
		if err != nil {
			return out, "macho", binary.LittleEndian
		}
		for _, s := range f.Sections {
			// Mach-O DWARF sections live in the __DWARF segment and are named
			// like "__debug_info"; normalise to ".debug_info".
			if strings.HasPrefix(s.Name, "__debug") {
				if d, err := s.Data(); err == nil {
					out[normaliseDebugName(s.Name)] = d
				}
			}
		}
		return out, "macho", f.ByteOrder

	case len(data) >= 2 && binary.LittleEndian.Uint16(data[0:2]) == 0x5a4d:
		f, err := pe.NewFile(bytesToReader(data))
		if err != nil {
			return out, "pe", binary.LittleEndian
		}
		for _, s := range f.Sections {
			if strings.HasPrefix(s.Name, ".debug") {
				if d, err := s.Data(); err == nil {
					out[normaliseDebugName(s.Name)] = d
				}
			}
		}
		return out, "pe", binary.LittleEndian
	}

	return out, "", binary.LittleEndian
}

// normaliseDebugName canonicalises a platform-specific debug section name to
// its DWARF form (".debug_info"). Mach-O uses a "__debug_info" convention and
// ELF may emit compressed ".zdebug_info" sections.
func normaliseDebugName(name string) string {
	if strings.HasPrefix(name, "__") {
		return "." + name[2:]
	}
	if strings.HasPrefix(name, ".zdebug") {
		return strings.Replace(name, ".zdebug", ".debug", 1)
	}
	return name
}
