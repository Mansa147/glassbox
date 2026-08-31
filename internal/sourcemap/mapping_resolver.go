// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package sourcemap

// MappingResolver scores WASM-to-source mappings and attaches source links
// according to confidence thresholds.
type MappingResolver struct {
	mapper    *FallbackMapper
	startPath string
	external  *ExternalRepoRegistry
}

// NewMappingResolver creates a resolver that uses projectRoot for path resolution
// and optional external repo mappings for cross-repository links.
func NewMappingResolver(projectRoot string, external *ExternalRepoRegistry) *MappingResolver {
	return &MappingResolver{
		mapper:    NewFallbackMapper(projectRoot),
		startPath: projectRoot,
		external:  external,
	}
}

// Resolve maps wasmData+addr to a source location, applies confidence scoring,
// and populates SourceLink or CandidateLink when link presentation allows it.
func (r *MappingResolver) Resolve(wasmData []byte, addr uint64) *FallbackResult {
	result := r.mapper.Resolve(wasmData, addr)
	if result == nil {
		return nil
	}
	r.attachSourceLink(result)
	return result
}

func (r *MappingResolver) attachSourceLink(result *FallbackResult) {
	if result.File == "" {
		return
	}
	switch result.LinkPresentation {
	case LinkAuto, LinkCandidate:
	default:
		return
	}
	url, provenance, err := ResolveGitHubURLWithProvenance(r.startPath, result.File, r.external, RevisionOptions{})
	result.LinkProvenance = provenance.Label()
	if err != nil {
		// Keep the label in machine-readable mapping output even when policy
		// correctly refuses to create a potentially misleading link.
		return
	}
	if result.LinkPresentation == LinkAuto {
		result.SourceLink = url
		return
	}
	result.CandidateLink = url
}
