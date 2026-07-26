// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ImportConflictPolicy selects how ImportSession resolves an incoming
// session whose ID collides with one already in the store.
type ImportConflictPolicy string

const (
	// ImportFail rejects the import outright when a conflict is found. This
	// is the default, non-destructive policy — it never overwrites or
	// deletes the existing session.
	ImportFail ImportConflictPolicy = "fail"
	// ImportRename assigns the incoming session a freshly generated ID so
	// it is stored alongside the existing one without touching it.
	ImportRename ImportConflictPolicy = "rename"
	// ImportMerge combines mergeable metadata (bookmark name, annotations)
	// from both records under the existing session's identity.
	ImportMerge ImportConflictPolicy = "merge"
)

// ParseImportConflictPolicy validates a user-supplied policy string (e.g.
// from a --on-conflict CLI flag).
func ParseImportConflictPolicy(s string) (ImportConflictPolicy, error) {
	switch ImportConflictPolicy(strings.ToLower(strings.TrimSpace(s))) {
	case ImportFail:
		return ImportFail, nil
	case ImportRename:
		return ImportRename, nil
	case ImportMerge:
		return ImportMerge, nil
	default:
		return "", fmt.Errorf(
			"unknown import conflict policy %q — must be one of: fail, rename, merge",
			s,
		)
	}
}

// ImportConflict describes one field that differs between an incoming
// session and the existing session sharing its ID.
type ImportConflict struct {
	Field    string
	Existing string
	Incoming string
}

// DetectImportConflict compares an incoming session's identity against the
// store's current record for the same ID. It returns the existing record
// (nil when there is none — i.e. no conflict) along with every field-level
// difference, so callers can render a conflict preview before choosing a
// resolution policy.
func DetectImportConflict(ctx context.Context, store *Store, incoming *Data) (*Data, []ImportConflict, error) {
	if incoming == nil {
		return nil, nil, fmt.Errorf("incoming session data is nil")
	}
	if strings.TrimSpace(incoming.ID) == "" {
		return nil, nil, fmt.Errorf("incoming session has no ID")
	}

	existing, err := store.Load(ctx, incoming.ID)
	if err != nil {
		// Not found (or any load failure) means no existing record to
		// conflict with.
		return nil, nil, nil
	}

	var conflicts []ImportConflict
	diff := func(field, existingV, incomingV string) {
		if existingV != incomingV {
			conflicts = append(conflicts, ImportConflict{Field: field, Existing: existingV, Incoming: incomingV})
		}
	}
	diff("Name", existing.Name, incoming.Name)
	diff("TxHash", existing.TxHash, incoming.TxHash)
	diff("Network", existing.Network, incoming.Network)
	diff("Status", existing.Status, incoming.Status)
	diff("AnnotationsJSON", existing.AnnotationsJSON, incoming.AnnotationsJSON)

	return existing, conflicts, nil
}

// ImportResult summarizes the outcome of ImportSession.
type ImportResult struct {
	// Policy is the resolution policy that was applied.
	Policy ImportConflictPolicy
	// Existing is the pre-import record that collided with incoming's ID,
	// or nil when there was no conflict.
	Existing *Data
	// Conflicts lists every field that differed between Existing and the
	// incoming session, regardless of which policy was applied.
	Conflicts []ImportConflict
	// Saved is the record actually persisted to the store.
	Saved *Data
	// Renamed is true when policy assigned the incoming session a new ID.
	Renamed bool
	// Merged is true when policy combined incoming and existing metadata.
	Merged bool
}

// ImportSession persists incoming into the store, resolving an ID collision
// with policy. When there is no conflict, incoming is saved unchanged
// regardless of policy. The default policy (ImportFail) never overwrites or
// deletes existing data.
func (s *Store) ImportSession(ctx context.Context, incoming *Data, policy ImportConflictPolicy) (*ImportResult, error) {
	if incoming == nil {
		return nil, fmt.Errorf("cannot import nil session data")
	}

	existing, conflicts, err := DetectImportConflict(ctx, s, incoming)
	if err != nil {
		return nil, err
	}

	result := &ImportResult{Policy: policy, Existing: existing, Conflicts: conflicts}

	if existing == nil {
		if err := s.SaveWithValidation(ctx, incoming); err != nil {
			return nil, err
		}
		result.Saved = incoming
		return result, nil
	}

	switch policy {
	case ImportRename:
		renamed := *incoming
		renamed.ID = GenerateID(incoming.TxHash)
		// Guard against the vanishingly unlikely case that the freshly
		// generated ID also collides with a third session.
		for attempts := 0; attempts < 5; attempts++ {
			if _, loadErr := s.Load(ctx, renamed.ID); loadErr != nil {
				break
			}
			renamed.ID = GenerateID(incoming.TxHash)
		}
		if err := s.SaveWithValidation(ctx, &renamed); err != nil {
			return nil, err
		}
		result.Saved = &renamed
		result.Renamed = true
		return result, nil

	case ImportMerge:
		merged := mergeSessionMetadata(existing, incoming)
		if err := s.SaveWithValidation(ctx, merged); err != nil {
			return nil, err
		}
		result.Saved = merged
		result.Merged = true
		return result, nil

	case ImportFail, "":
		return result, formatImportConflictError(incoming.ID, conflicts)

	default:
		return nil, fmt.Errorf("unknown import conflict policy %q", policy)
	}
}

// formatImportConflictError renders an import conflict in the same
// numbered-list style used elsewhere in this package for diagnostics.
func formatImportConflictError(id string, conflicts []ImportConflict) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"import conflict: session %q already exists (%d field(s) differ):\n",
		id, len(conflicts),
	))
	for i, c := range conflicts {
		sb.WriteString(fmt.Sprintf("  %d. [%s] existing=%q incoming=%q\n", i+1, c.Field, c.Existing, c.Incoming))
	}
	sb.WriteString("Re-run with --on-conflict rename or --on-conflict merge to resolve.")
	return fmt.Errorf("%s", sb.String())
}

// mergeSessionMetadata combines mergeable metadata from existing and
// incoming under existing's identity (ID, CreatedAt, audit chain), folding
// in bookmark and annotation data so neither side's work is silently lost.
func mergeSessionMetadata(existing, incoming *Data) *Data {
	merged := *existing

	// Bookmark name: keep existing's if set, otherwise adopt incoming's.
	if merged.Name == "" {
		merged.Name = incoming.Name
	}

	merged.AnnotationsJSON = mergeAnnotations(existing.AnnotationsJSON, incoming.AnnotationsJSON)

	if merged.EnvelopeXdr == "" {
		merged.EnvelopeXdr = incoming.EnvelopeXdr
	}
	if merged.SimResponseJSON == "" {
		merged.SimResponseJSON = incoming.SimResponseJSON
	}
	if incoming.LastAccessAt.After(merged.LastAccessAt) {
		merged.LastAccessAt = incoming.LastAccessAt
	}

	return &merged
}

// mergeAnnotations combines two JSON-array annotation payloads (each a list
// of note strings) into a single de-duplicated JSON array, preserving
// first-seen order. Unparseable input on either side is treated as empty
// rather than aborting the merge.
func mergeAnnotations(a, b string) string {
	combined := append(parseAnnotationList(a), parseAnnotationList(b)...)
	if len(combined) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(combined))
	deduped := make([]string, 0, len(combined))
	for _, item := range combined {
		if seen[item] {
			continue
		}
		seen[item] = true
		deduped = append(deduped, item)
	}
	out, err := json.Marshal(deduped)
	if err != nil {
		return ""
	}
	return string(out)
}

// parseAnnotationList decodes a JSON array of strings, returning nil for
// empty or unparseable input.
func parseAnnotationList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}
