// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package dwarf

import (
	"encoding/binary"
	"testing"
)

// ─── fixture helpers ────────────────────────────────────────────────────────

// encodeULEB128 encodes v as an unsigned LEB128 varint, matching the decoder in
// parser.go (readULEB128). Used to size hand-built WASM custom sections.
func encodeULEB128(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

// wasmCustomSection builds a single WASM custom section (id 0) whose name and
// content are the given values.
func wasmCustomSection(name string, content []byte) []byte {
	payload := append(encodeULEB128(uint64(len(name))), []byte(name)...)
	payload = append(payload, content...)

	sec := []byte{0x00} // custom section id
	sec = append(sec, encodeULEB128(uint64(len(payload)))...)
	sec = append(sec, payload...)
	return sec
}

// buildWASM assembles a WASM binary containing the given named debug sections.
func buildWASM(sections map[string][]byte) []byte {
	out := []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version 1
	}
	for name, content := range sections {
		out = append(out, wasmCustomSection(name, content)...)
	}
	return out
}

// debugInfoWithVersion returns a minimal .debug_info section whose 32-bit
// compilation-unit header advertises the given DWARF version.
func debugInfoWithVersion(version uint16) []byte {
	return []byte{
		0x20, 0x00, 0x00, 0x00, // unit_length (arbitrary, != 0xffffffff)
		byte(version), byte(version >> 8), // version (little-endian)
		0x00, 0x00, 0x00, 0x00, // debug_abbrev_offset
		0x04, // address_size
	}
}

// ─── tests ──────────────────────────────────────────────────────────────────

// TestDetectCapabilities_Stripped verifies a binary with no debug sections is
// reported as unsupported with a build-command hint (not a generic error).
func TestDetectCapabilities_Stripped(t *testing.T) {
	caps := DetectCapabilities(buildWASM(nil))

	if caps.BinaryType != "wasm" {
		t.Errorf("BinaryType = %q, want wasm", caps.BinaryType)
	}
	if len(caps.Sections) != 0 {
		t.Errorf("Sections = %v, want empty", caps.Sections)
	}
	if caps.Supported {
		t.Error("Supported = true, want false for a stripped binary")
	}
	if len(caps.Warnings) != 1 || caps.Warnings[0].Kind != WarnMissingDebugSections {
		t.Fatalf("warnings = %+v, want one WarnMissingDebugSections", caps.Warnings)
	}
	if caps.Warnings[0].Hint == "" {
		t.Error("missing-sections warning should carry a build-command hint")
	}
}

// TestDetectCapabilities_Modern verifies a modern DWARF v5 binary with the full
// section set is fully supported with no warnings and behaves as before.
func TestDetectCapabilities_Modern(t *testing.T) {
	caps := DetectCapabilities(buildWASM(map[string][]byte{
		".debug_info":   debugInfoWithVersion(5),
		".debug_abbrev": {0x01, 0x02, 0x03},
		".debug_line":   {0x01, 0x02, 0x03},
	}))

	if caps.Version != 5 {
		t.Errorf("Version = %d, want 5", caps.Version)
	}
	if !caps.Supported {
		t.Error("Supported = false, want true for modern DWARF v5")
	}
	if len(caps.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", caps.Warnings)
	}
	if !caps.Mappings.SourceLines || !caps.Mappings.LocalVars || !caps.Mappings.InlineFrames {
		t.Errorf("mappings = %+v, want all true", caps.Mappings)
	}
}

// TestDetectCapabilities_Partial verifies a binary with only .debug_line (info
// stripped) reports partial support: line mapping works, variable extraction
// does not — distinct from a missing-symbols case.
func TestDetectCapabilities_Partial(t *testing.T) {
	caps := DetectCapabilities(buildWASM(map[string][]byte{
		".debug_line": {0x01, 0x02, 0x03},
	}))

	if caps.Version != 0 {
		t.Errorf("Version = %d, want 0 (no .debug_info)", caps.Version)
	}
	if !caps.Mappings.SourceLines {
		t.Error("SourceLines = false, want true when .debug_line is present")
	}
	if caps.Mappings.LocalVars {
		t.Error("LocalVars = true, want false without .debug_info/.debug_abbrev")
	}
	if !caps.Supported {
		t.Error("Supported = false, want true (partial line mapping available)")
	}
	if len(caps.Warnings) != 1 || caps.Warnings[0].Kind != WarnPartialDebugInfo {
		t.Fatalf("warnings = %+v, want one WarnPartialDebugInfo", caps.Warnings)
	}
}

// TestDetectCapabilities_UnsupportedOld verifies a DWARF v1 binary names the
// version and the supported range, and reports .debug_info mapping unavailable.
func TestDetectCapabilities_UnsupportedOld(t *testing.T) {
	caps := DetectCapabilities(buildWASM(map[string][]byte{
		".debug_info":   debugInfoWithVersion(1),
		".debug_abbrev": {0x01},
		".debug_line":   {0x01},
	}))

	if caps.Version != 1 {
		t.Fatalf("Version = %d, want 1", caps.Version)
	}
	if len(caps.Warnings) != 1 || caps.Warnings[0].Kind != WarnUnsupportedVersion {
		t.Fatalf("warnings = %+v, want one WarnUnsupportedVersion", caps.Warnings)
	}
	if caps.Mappings.LocalVars {
		t.Error("LocalVars = true, want false for an unsupported version")
	}
}

// TestDetectCapabilities_UnsupportedNew verifies a future DWARF v6 binary is
// reported as newer than the supported range.
func TestDetectCapabilities_UnsupportedNew(t *testing.T) {
	caps := DetectCapabilities(buildWASM(map[string][]byte{
		".debug_info": debugInfoWithVersion(6),
	}))

	if caps.Version != 6 {
		t.Fatalf("Version = %d, want 6", caps.Version)
	}
	if len(caps.Warnings) == 0 || caps.Warnings[0].Kind != WarnUnsupportedVersion {
		t.Fatalf("warnings = %+v, want WarnUnsupportedVersion", caps.Warnings)
	}
}

// TestDetectCapabilities_MissingAbbrev verifies that .debug_info present without
// .debug_abbrev is reported as partial (variable extraction unavailable), not
// as fully supported.
func TestDetectCapabilities_MissingAbbrev(t *testing.T) {
	caps := DetectCapabilities(buildWASM(map[string][]byte{
		".debug_info": debugInfoWithVersion(4),
		".debug_line": {0x01},
	}))

	if caps.Mappings.LocalVars {
		t.Error("LocalVars = true, want false without .debug_abbrev")
	}
	if !caps.Mappings.SourceLines {
		t.Error("SourceLines = false, want true with .debug_line + supported version")
	}
	found := false
	for _, w := range caps.Warnings {
		if w.Kind == WarnPartialDebugInfo {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %+v, want a WarnPartialDebugInfo", caps.Warnings)
	}
}

// TestDetectCapabilities_Unrecognised verifies non-binary input yields no
// binary type and a diagnostic rather than a panic.
func TestDetectCapabilities_Unrecognised(t *testing.T) {
	caps := DetectCapabilities([]byte("not a real binary at all"))
	if caps.BinaryType != "" {
		t.Errorf("BinaryType = %q, want empty", caps.BinaryType)
	}
	if caps.Supported {
		t.Error("Supported = true, want false for unrecognised input")
	}
	if len(caps.Warnings) == 0 {
		t.Error("want at least one warning for unrecognised input")
	}
}

// TestDetectCapabilities_ShortInput verifies inputs too short to classify are
// handled gracefully.
func TestDetectCapabilities_ShortInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}, {0x00}, {0x00, 0x61}} {
		caps := DetectCapabilities(in)
		if caps == nil {
			t.Fatalf("DetectCapabilities(%v) = nil", in)
		}
		if caps.Supported {
			t.Errorf("DetectCapabilities(%v).Supported = true, want false", in)
		}
	}
}

// TestReadDWARFVersion_64Bit verifies the 64-bit DWARF unit header (0xffffffff
// escape + 8-byte length) is parsed for its version field.
func TestReadDWARFVersion_64Bit(t *testing.T) {
	info := []byte{
		0xff, 0xff, 0xff, 0xff, // 64-bit escape
		0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // unit_length (8 bytes)
		0x05, 0x00, // version 5
	}
	if v := readDWARFVersion(info, binary.LittleEndian); v != 5 {
		t.Errorf("readDWARFVersion(64-bit) = %d, want 5", v)
	}
}

// TestCapabilities_Summary exercises the human-readable summary for the main
// cases so log output stays sensible.
func TestCapabilities_Summary(t *testing.T) {
	stripped := DetectCapabilities(buildWASM(nil))
	if got := stripped.Summary(); got == "" {
		t.Error("Summary() returned empty for stripped binary")
	}
	modern := DetectCapabilities(buildWASM(map[string][]byte{
		".debug_info": debugInfoWithVersion(5),
	}))
	if !modern.HasSection(".debug_info") {
		t.Error("HasSection(.debug_info) = false, want true")
	}
}

// TestParser_CapabilitiesAccessor verifies Parser.Capabilities returns the
// report attached at construction time.
func TestParser_CapabilitiesAccessor(t *testing.T) {
	data := buildWASM(map[string][]byte{
		".debug_info":   debugInfoWithVersion(5),
		".debug_abbrev": {0x01},
		".debug_line":   {0x01},
	})
	// Attach the report exactly as NewParser does. This exercises the accessor
	// without needing a fully valid DWARF tree that dwarf.New would accept.
	p := &Parser{caps: DetectCapabilities(data)}

	got := p.Capabilities()
	if got == nil {
		t.Fatal("Capabilities() = nil, want a report")
	}
	if got.Version != 5 {
		t.Errorf("Capabilities().Version = %d, want 5", got.Version)
	}
	if !got.Supported {
		t.Error("Capabilities().Supported = false, want true")
	}
}

// TestParser_CapabilitiesNil verifies the accessor is safe on a parser with no
// report attached (e.g. one built directly in a test).
func TestParser_CapabilitiesNil(t *testing.T) {
	p := &Parser{}
	if p.Capabilities() != nil {
		t.Error("Capabilities() = non-nil, want nil for a parser with no report")
	}
}
