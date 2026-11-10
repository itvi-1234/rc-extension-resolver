package resolver

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadAndMerge_MappedDependencyGraph(t *testing.T) {
	locations := map[string]string{uriA: "mem://root.yaml", uriB: "mem://dependency.yaml"}
	docs := map[string][]byte{
		locations[uriA]: []byte("metadata: {id: independent.root, version: independent-version}\nspec:\n  dependencies: [" + uriB + "]\n  kinds: [{name: configuration}]\n"),
		locations[uriB]: []byte("metadata: {id: independent.dependency}\nspec:\n  kinds: [{name: api}]\n  interfaceTypes: [{name: http, targetKind: api}]\n"),
	}
	catalog, err := LoadAndMerge(NewInMemoryLoader(docs), uriA, locations)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range []struct{ kind, owner string }{{"configuration", uriA}, {"api", uriB}} {
		owner, _, err := catalog.resolveKind(declaration.kind, declaration.owner)
		if err != nil || owner != declaration.owner {
			t.Fatalf("kind %q lost canonical owner %q: owner=%q error=%v", declaration.kind, declaration.owner, owner, err)
		}
	}
	if !catalog.IsValidInterfaceType("api", "http") {
		t.Fatal("dependency interface type was not merged")
	}
}

func TestLoadAndMerge_OptionalMappings(t *testing.T) {
	docs := map[string][]byte{
		"mem://root.yaml": []byte("metadata: {id: " + uriA + "}\nspec: {dependencies: [" + uriB + "]}\n"),
		uriA:              []byte("metadata: {id: independent.root}\nspec: {dependencies: [" + uriB + "]}\n"),
		uriB:              []byte("metadata: {id: independent.dependency}\nspec: {kinds: [{name: api}]}\n"),
	}
	for _, test := range []struct {
		name      string
		locations map[string]string
		valid     bool
	}{
		{"direct loading", nil, true},
		{"mapped root and direct dependency", map[string]string{uriA: "mem://root.yaml"}, true},
		{"empty explicit mapping", map[string]string{uriA: ""}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := LoadAndMerge(NewInMemoryLoader(docs), uriA, test.locations)
			if test.valid {
				if err != nil || !catalog.IsValidKind("api") {
					t.Fatalf("optional mapping failed: %v", err)
				}
			} else if catalog != nil || err == nil || !strings.Contains(err.Error(), "empty retrieval location") {
				t.Fatalf("expected empty mapping failure, got catalog=%v error=%v", catalog, err)
			}
		})
	}
}

func TestLoadAndMerge_OfficialReferenceOwnsConditions(t *testing.T) {
	const reference = "https://runtimeconditions.io/extensions/common-integrations:v1alpha1"
	const artifact = "https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml"
	docs := map[string][]byte{artifact: []byte("metadata: {id: " + artifact + ", version: v1alpha1}\nspec:\n  kinds: [{name: api}]\n  interfaceTypes: [{name: http, targetKind: api}]\n  schemas:\n    - id: api-http\n      appliesToKind: api\n      appliesToInterfaceType: http\n      schema: {type: object}\n")}
	catalog, err := LoadAndMerge(NewInMemoryLoader(docs), reference, nil)
	if err != nil {
		t.Fatal(err)
	}
	condition := map[string]any{"kind": "api", "extension": reference, "interface": map[string]any{"type": "http", "spec": map[string]any{}}}
	result, err := catalog.ValidateCondition(condition)
	if err != nil || !result.Valid || result.ResolvedExtension != reference {
		t.Fatalf("condition lost its profile reference: %+v, %v", result, err)
	}
	if entries := catalog.schemasFor("api", "http"); len(entries) != 1 || entries[0].ownerURI != reference {
		t.Fatal("schema ownership used definition metadata instead of the profile reference")
	}
	condition["extension"] = artifact
	var mismatch *ExtensionMismatchError
	if _, err := catalog.ValidateCondition(condition); !errors.As(err, &mismatch) {
		t.Fatalf("definition identity was accepted as a profile selector: %v", err)
	}
}

func TestMerge_CombinedGraphsKeepReferenceOwnership(t *testing.T) {
	docs := map[string][]byte{
		"example.first":  []byte("metadata: {id: shared.definition, version: '1'}\nspec: {kinds: [{name: shared}]}\n"),
		"example.second": []byte("metadata: {id: shared.definition, version: '2'}\nspec: {kinds: [{name: shared}]}\n"),
	}
	combined := &ResolvedGraph{ByID: map[string]*ExtensionDefinition{}}
	for _, reference := range []string{"example.first", "example.second"} {
		graph, err := NewResolver(NewInMemoryLoader(docs)).Resolve(reference)
		if err != nil {
			t.Fatal(err)
		}
		combined.Extensions = append(combined.Extensions, graph.Extensions...)
		for key, definition := range graph.ByID {
			combined.ByID[key] = definition
		}
	}
	catalog, err := Merge(combined)
	if err != nil {
		t.Fatal(err)
	}
	for reference := range docs {
		if owner, _, err := catalog.resolveKind("shared", reference); err != nil || owner != reference {
			t.Fatalf("combined graph lost reference %q: owner=%q error=%v", reference, owner, err)
		}
	}
}

func TestLoadAndMerge_PropagatesFetchError(t *testing.T) {
	const location = "mem://missing.yaml"
	catalog, err := LoadAndMerge(NewInMemoryLoader(nil), uriA, map[string]string{uriA: location})
	var fetch *FetchError
	if catalog != nil || !errors.As(err, &fetch) || fetch.URI != location || !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected mapped fetch failure, got catalog=%v error=%v", catalog, err)
	}
}

func TestLoadAndMerge_PropagatesMergeConflict(t *testing.T) {
	locations := map[string]string{uriA: "mem://root.yaml", uriB: "mem://dependency.yaml"}
	const declarations = "  interfaceTypes: [{name: http, targetKind: api}]\n"
	docs := map[string][]byte{
		locations[uriA]: []byte("metadata: {id: " + uriA + "}\nspec:\n  dependencies: [" + uriB + "]\n" + declarations),
		locations[uriB]: []byte("metadata: {id: " + uriB + "}\nspec:\n" + declarations),
	}
	catalog, err := LoadAndMerge(NewInMemoryLoader(docs), uriA, locations)
	var conflict *ConflictError
	if catalog != nil || !errors.As(err, &conflict) || conflict.Kind != "api" || conflict.InterfaceType != "http" {
		t.Fatalf("expected merge conflict, got catalog=%v error=%v", catalog, err)
	}
}

func TestMerge_DistinctKindsAcrossExtensions(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  kinds: [{name: gadget}]
`),
	}
	g := mustResolve(t, docs, uriA)
	c, err := Merge(g)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	if !c.IsValidKind("widget") || !c.IsValidKind("gadget") {
		t.Fatalf("expected both kinds valid, got %v", c)
	}
	if !c.IsValidInterfaceType("widget", "http") {
		t.Fatal("expected widget/http interface type to be valid")
	}
	if c.IsValidKind("nope") {
		t.Fatal("expected unknown kind to be invalid")
	}
}

func TestMerge_AmbiguousKindIsAllowed(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  kinds: [{name: widget}]
`),
	}
	g := mustResolve(t, docs, uriA)
	c, err := Merge(g)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	if !c.IsValidKind("widget") {
		t.Fatal("expected widget to be a valid kind")
	}
}

func TestMerge_ConflictingInterfaceTypeDeclarationFails(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  interfaceTypes: [{name: http, targetKind: widget}]
`),
	}
	g := mustResolve(t, docs, uriA)
	_, err := Merge(g)
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
	if conflictErr.InterfaceType != "http" {
		t.Fatalf("expected conflict on interface type http, got %q", conflictErr.InterfaceType)
	}
}

func TestMerge_ConflictingSchemaBindingFails(t *testing.T) {
	schemaDoc := `
  schemas:
    - id: widget-http
      appliesToKind: widget
      appliesToInterfaceType: http
      schema: {type: object}
`
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
` + schemaDoc),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
` + schemaDoc),
	}
	g := mustResolve(t, docs, uriA)
	_, err := Merge(g)
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
}
