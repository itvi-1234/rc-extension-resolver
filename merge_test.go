package resolver

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadAndMerge_MappedDependencyGraph(t *testing.T) {
	locations := map[string]string{uriA: "mem://root.yaml", uriB: "mem://dependency.yaml"}
	docs := map[string][]byte{
		locations[uriA]: []byte("metadata: {id: " + uriA + ", version: '1'}\nspec:\n  dependencies: [" + uriB + "]\n  kinds: [{name: configuration}]\n"),
		locations[uriB]: []byte("metadata: {id: " + uriB + "}\nspec:\n  kinds: [{name: api}]\n  interfaceTypes: [{name: http, targetKind: api}]\n"),
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

func TestLoadAndMerge_RequiresRootAndDependencyMappings(t *testing.T) {
	docs := map[string][]byte{
		"mem://root.yaml": []byte("metadata: {id: " + uriA + "}\nspec: {dependencies: [" + uriB + "]}\n"),
	}
	for _, test := range []struct {
		name, message string
		locations     map[string]string
	}{
		{"missing root", "no retrieval mapping", nil},
		{"empty root", "empty retrieval location", map[string]string{uriA: ""}},
		{"missing dependency", "no retrieval mapping", map[string]string{uriA: "mem://root.yaml"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := LoadAndMerge(NewInMemoryLoader(docs), uriA, test.locations)
			if catalog != nil || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected mapping failure %q, got catalog=%v error=%v", test.message, catalog, err)
			}
		})
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
