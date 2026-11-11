package resolver

import (
	"strings"
	"testing"
)

const (
	spellingShortCommon = "https://runtimeconditions.io/extensions/common-integrations:v1alpha1"
	spellingFullCommon  = "https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml"
	spellingShortEnv    = "https://runtimeconditions.io/extensions/env-configuration:v1alpha1"
	spellingFullEnv     = "https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml"
)

// env-configuration depends on common-integrations by its full id, while a
// profile may list both extensions by their short <url>:<version> form.
func spellingDocs() map[string][]byte {
	return map[string][]byte{
		spellingFullCommon: []byte("metadata: {id: " + spellingFullCommon + ", version: v1alpha1}\n" +
			"spec:\n  kinds: [{name: datastore}]\n  interfaceTypes: [{name: relational, targetKind: datastore}]\n" +
			"  schemas:\n    - id: datastore-relational\n      appliesToKind: datastore\n      appliesToInterfaceType: relational\n      schema: {type: object}\n"),
		spellingFullEnv: []byte("metadata: {id: " + spellingFullEnv + ", version: v1alpha1}\n" +
			"spec:\n  dependencies: [" + spellingFullCommon + "]\n"),
	}
}

func TestResolveAll_RootsShareOneGraphKeyedByReference(t *testing.T) {
	g, err := NewResolver(NewInMemoryLoader(spellingDocs())).ResolveAll(spellingShortCommon, spellingShortEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{spellingShortCommon, spellingShortEnv, spellingFullCommon} {
		if g.ByID[reference] == nil {
			t.Errorf("graph lost reference %q", reference)
		}
	}
	if _, err := Merge(g); err != nil {
		t.Fatalf("one extension under two spellings must not conflict: %v", err)
	}
}

func TestResolveAll_SameReferenceTwiceIsOneEntry(t *testing.T) {
	g, err := NewResolver(NewInMemoryLoader(spellingDocs())).ResolveAll(spellingShortCommon, spellingShortCommon)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Extensions) != 1 || len(g.ByID) != 1 {
		t.Fatalf("expected one entry, got %d extensions and %d references", len(g.Extensions), len(g.ByID))
	}
}

func TestResolveAll_FailsOnTheFirstBrokenRoot(t *testing.T) {
	if _, err := NewResolver(NewInMemoryLoader(spellingDocs())).ResolveAll(spellingShortCommon, "https://runtimeconditions.io/extensions/missing:v1"); err == nil {
		t.Fatal("expected an error for the unresolvable root")
	}
}

func TestResolve_RecordsRetrievalLocation(t *testing.T) {
	g, err := NewResolver(NewInMemoryLoader(spellingDocs())).Resolve(spellingShortCommon)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.ByID[spellingShortCommon].Location; got != spellingFullCommon {
		t.Fatalf("Location = %q, want %q", got, spellingFullCommon)
	}
}

// A graph combined from separate Resolve calls, keyed by reference. This is
// how a consumer that resolves each profile extension on its own sees it.
func combinedByReference(t *testing.T, roots ...string) *ResolvedGraph {
	t.Helper()
	combined := &ResolvedGraph{ByID: map[string]*ExtensionDefinition{}}
	for _, root := range roots {
		g, err := NewResolver(NewInMemoryLoader(spellingDocs())).Resolve(root)
		if err != nil {
			t.Fatal(err)
		}
		combined.Extensions = append(combined.Extensions, g.Extensions...)
		for reference, definition := range g.ByID {
			combined.ByID[reference] = definition
		}
	}
	return combined
}

func TestMerge_TwoSpellingsOfOneDefinitionDoNotConflict(t *testing.T) {
	if _, err := Merge(combinedByReference(t, spellingShortCommon, spellingShortEnv)); err != nil {
		t.Fatalf("same file reached as %q and %q must not conflict: %v", spellingShortCommon, spellingFullCommon, err)
	}
}

func TestMerge_OneDefinitionUnderTwoReferencesDoesNotConflict(t *testing.T) {
	g, err := NewResolver(NewInMemoryLoader(spellingDocs())).Resolve(spellingFullCommon)
	if err != nil {
		t.Fatal(err)
	}
	g.ByID[spellingShortCommon] = g.ByID[spellingFullCommon]
	catalog, err := Merge(g)
	if err != nil {
		t.Fatalf("a definition with interface types and schemas conflicted with itself: %v", err)
	}
	if n := len(catalog.schemasFor("datastore", "relational")); n != 1 {
		t.Fatalf("schema bound %d times, want once", n)
	}
}

func TestMerge_ExplicitExtensionMatchesEitherSpelling(t *testing.T) {
	catalog, err := Merge(combinedByReference(t, spellingShortCommon, spellingShortEnv))
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{spellingShortCommon, spellingFullCommon} {
		if owner, match, err := catalog.resolveKind("datastore", reference); err != nil || owner != reference || match != KindMatchExplicit {
			t.Errorf("resolveKind(%q) = %q, %q, %v", reference, owner, match, err)
		}
	}
}

func TestMerge_TwoSpellingsAreNotAnAmbiguousKind(t *testing.T) {
	catalog, err := Merge(combinedByReference(t, spellingShortCommon, spellingShortEnv))
	if err != nil {
		t.Fatal(err)
	}
	if _, match, err := catalog.resolveKind("datastore", ""); err != nil || match != "" {
		t.Fatalf("one definition reached under two spellings reported match %q, error %v", match, err)
	}
}

func TestMerge_DefinitionsFromDifferentLocationsStillConflict(t *testing.T) {
	docs := map[string][]byte{
		"example.first":  []byte("metadata: {id: first}\nspec:\n  kinds: [{name: widget}]\n  interfaceTypes: [{name: http, targetKind: widget}]\n"),
		"example.second": []byte("metadata: {id: second}\nspec:\n  kinds: [{name: widget}]\n  interfaceTypes: [{name: http, targetKind: widget}]\n"),
	}
	combined := &ResolvedGraph{ByID: map[string]*ExtensionDefinition{}}
	for _, reference := range []string{"example.first", "example.second"} {
		g, err := NewResolver(NewInMemoryLoader(docs)).Resolve(reference)
		if err != nil {
			t.Fatal(err)
		}
		combined.Extensions = append(combined.Extensions, g.Extensions...)
		for key, definition := range g.ByID {
			combined.ByID[key] = definition
		}
	}
	if _, err := Merge(combined); err == nil || !strings.Contains(err.Error(), "widget/http") {
		t.Fatalf("expected a conflict on widget/http, got %v", err)
	}
}
