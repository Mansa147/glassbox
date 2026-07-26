// Copyright 2026 Glassbox Users
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

func importTestData(id, name, txHash string) *Data {
	now := time.Now()
	return &Data{
		ID:            id,
		Name:          name,
		TxHash:        txHash,
		Network:       "testnet",
		HorizonURL:    "https://horizon-testnet.stellar.org",
		Status:        "saved",
		CreatedAt:     now.Add(-time.Hour),
		LastAccessAt:  now,
		SchemaVersion: SchemaVersion,
	}
}

// ── ParseImportConflictPolicy ─────────────────────────────────────────────────

func TestParseImportConflictPolicy_ValidValues(t *testing.T) {
	for _, s := range []string{"fail", "rename", "merge", "FAIL", " Merge "} {
		if _, err := ParseImportConflictPolicy(s); err != nil {
			t.Errorf("ParseImportConflictPolicy(%q) unexpected error: %v", s, err)
		}
	}
}

func TestParseImportConflictPolicy_InvalidValue(t *testing.T) {
	if _, err := ParseImportConflictPolicy("overwrite"); err == nil {
		t.Fatal("expected error for unknown policy")
	}
}

// ── No conflict ───────────────────────────────────────────────────────────────

func TestImportSession_NoConflict_SavesDirectly(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	incoming := importTestData("fresh-session", "", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	result, err := store.ImportSession(ctx, incoming, ImportFail)
	if err != nil {
		t.Fatalf("ImportSession: %v", err)
	}
	if result.Existing != nil {
		t.Error("expected no existing record for a fresh ID")
	}
	if _, loadErr := store.Load(ctx, "fresh-session"); loadErr != nil {
		t.Errorf("expected the session to be persisted: %v", loadErr)
	}
}

// ── Fail policy (default) ────────────────────────────────────────────────────

func TestImportSession_FailPolicy_NonDestructive(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	original := importTestData("dup-session", "original-bookmark", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if err := store.SaveWithValidation(ctx, original); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	incoming := importTestData("dup-session", "incoming-bookmark", "1111111111111111111111111111111111111111111111111111111111111111"[:64])
	_, err = store.ImportSession(ctx, incoming, ImportFail)
	if err == nil {
		t.Fatal("expected an error for a conflicting import under the fail policy")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("error should mention 'conflict', got: %v", err)
	}
	if !strings.Contains(err.Error(), "--on-conflict") {
		t.Errorf("error should hint at --on-conflict, got: %v", err)
	}

	// The existing session must be completely untouched.
	loaded, loadErr := store.Load(ctx, "dup-session")
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if loaded.Name != "original-bookmark" {
		t.Errorf("fail policy must not modify the existing session, got Name=%q", loaded.Name)
	}
}

func TestImportSession_DefaultPolicyIsFail(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	original := importTestData("dup-session-2", "keep-me", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if err := store.SaveWithValidation(ctx, original); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	incoming := importTestData("dup-session-2", "different", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if _, err := store.ImportSession(ctx, incoming, ""); err == nil {
		t.Fatal("expected the empty/default policy to behave like fail and reject the import")
	}
}

func TestImportSession_ConflictListsEachDifferingField(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	original := importTestData("dup-session-3", "name-a", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if err := store.SaveWithValidation(ctx, original); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	incoming := importTestData("dup-session-3", "name-b", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	result, err := store.ImportSession(ctx, incoming, ImportFail)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	found := false
	for _, c := range result.Conflicts {
		if c.Field == "Name" {
			found = true
			if c.Existing != "name-a" || c.Incoming != "name-b" {
				t.Errorf("unexpected conflict values: %+v", c)
			}
		}
	}
	if !found {
		t.Errorf("expected a Name conflict to be listed, got: %+v", result.Conflicts)
	}
}

// ── Rename policy ─────────────────────────────────────────────────────────────

func TestImportSession_RenamePolicy_KeepsBothSessions(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	original := importTestData("dup-session-4", "original", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if err := store.SaveWithValidation(ctx, original); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	incoming := importTestData("dup-session-4", "incoming", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	result, err := store.ImportSession(ctx, incoming, ImportRename)
	if err != nil {
		t.Fatalf("ImportSession rename: %v", err)
	}
	if !result.Renamed {
		t.Error("expected Renamed=true")
	}
	if result.Saved.ID == "dup-session-4" {
		t.Error("expected a freshly generated ID distinct from the original")
	}

	// Both sessions must now exist independently.
	if _, err := store.Load(ctx, "dup-session-4"); err != nil {
		t.Errorf("original session must survive rename import: %v", err)
	}
	if _, err := store.Load(ctx, result.Saved.ID); err != nil {
		t.Errorf("renamed session must be persisted: %v", err)
	}

	loadedOriginal, _ := store.Load(ctx, "dup-session-4")
	if loadedOriginal.Name != "original" {
		t.Errorf("rename policy must not alter the original session, got Name=%q", loadedOriginal.Name)
	}
}

// ── Merge policy ──────────────────────────────────────────────────────────────

func TestImportSession_MergePolicy_CombinesAnnotationsAndKeepsExistingName(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	original := importTestData("dup-session-5", "existing-bookmark", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	original.AnnotationsJSON = `["note-a"]`
	if err := store.SaveWithValidation(ctx, original); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	incoming := importTestData("dup-session-5", "incoming-bookmark", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	incoming.AnnotationsJSON = `["note-b"]`

	result, err := store.ImportSession(ctx, incoming, ImportMerge)
	if err != nil {
		t.Fatalf("ImportSession merge: %v", err)
	}
	if !result.Merged {
		t.Error("expected Merged=true")
	}
	if result.Saved.ID != "dup-session-5" {
		t.Errorf("merge policy must keep the existing ID, got %q", result.Saved.ID)
	}
	if result.Saved.Name != "existing-bookmark" {
		t.Errorf("merge policy should keep the existing bookmark name when set, got %q", result.Saved.Name)
	}
	if !strings.Contains(result.Saved.AnnotationsJSON, "note-a") || !strings.Contains(result.Saved.AnnotationsJSON, "note-b") {
		t.Errorf("expected merged annotations to contain both notes, got %q", result.Saved.AnnotationsJSON)
	}

	loaded, err := store.Load(ctx, "dup-session-5")
	if err != nil {
		t.Fatalf("Load merged session: %v", err)
	}
	if !strings.Contains(loaded.AnnotationsJSON, "note-a") || !strings.Contains(loaded.AnnotationsJSON, "note-b") {
		t.Errorf("persisted merged session should contain both notes, got %q", loaded.AnnotationsJSON)
	}
}

func TestImportSession_MergePolicy_AdoptsIncomingNameWhenExistingEmpty(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	original := importTestData("dup-session-6", "", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if err := store.SaveWithValidation(ctx, original); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	incoming := importTestData("dup-session-6", "adopted-name", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	result, err := store.ImportSession(ctx, incoming, ImportMerge)
	if err != nil {
		t.Fatalf("ImportSession merge: %v", err)
	}
	if result.Saved.Name != "adopted-name" {
		t.Errorf("expected incoming's name to be adopted when existing has none, got %q", result.Saved.Name)
	}
}

// ── Repeated imports / partial overlap (integration) ─────────────────────────

func TestImportSession_RepeatedImports_PartialOverlap(t *testing.T) {
	overrideTempHome(t)
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	base := importTestData("repeat-session", "v1", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	base.AnnotationsJSON = `["a"]`
	if _, err := store.ImportSession(ctx, base, ImportFail); err != nil {
		t.Fatalf("initial import: %v", err)
	}

	// Re-importing the exact same data under "fail" must still conflict —
	// fail is non-destructive even for identical content re-imports.
	repeat := importTestData("repeat-session", "v1", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	repeat.AnnotationsJSON = `["a"]`
	if _, err := store.ImportSession(ctx, repeat, ImportFail); err == nil {
		t.Fatal("expected fail policy to reject a re-import even with identical content")
	}

	// Partial overlap: same ID, new annotation, different bookmark — merge
	// should combine rather than clobber.
	partial := importTestData("repeat-session", "v2", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	partial.AnnotationsJSON = `["b"]`
	result, err := store.ImportSession(ctx, partial, ImportMerge)
	if err != nil {
		t.Fatalf("merge import: %v", err)
	}
	if !strings.Contains(result.Saved.AnnotationsJSON, `"a"`) || !strings.Contains(result.Saved.AnnotationsJSON, `"b"`) {
		t.Errorf("expected merged annotations from both imports, got %q", result.Saved.AnnotationsJSON)
	}
	if result.Saved.Name != "v1" {
		t.Errorf("merge should keep the existing bookmark 'v1', got %q", result.Saved.Name)
	}

	// A further rename import must not disturb the merged record.
	renameIncoming := importTestData("repeat-session", "v3", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	renameResult, err := store.ImportSession(ctx, renameIncoming, ImportRename)
	if err != nil {
		t.Fatalf("rename import: %v", err)
	}
	if renameResult.Saved.ID == "repeat-session" {
		t.Error("rename import must receive a distinct ID")
	}
	mergedStillThere, err := store.Load(ctx, "repeat-session")
	if err != nil {
		t.Fatalf("Load repeat-session after rename import: %v", err)
	}
	if mergedStillThere.Name != "v1" {
		t.Errorf("rename import must not disturb the existing merged session, got Name=%q", mergedStillThere.Name)
	}
}
