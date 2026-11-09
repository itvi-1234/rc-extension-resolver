package resolver

import (
	"errors"
	"io"
	"reflect"
	"testing"
)

const (
	standardExtensionFilename = "runtimeconditions.extension.yaml"
	uriA                      = "mem://a.extension.yaml"
	uriB                      = "mem://b.extension.yaml"
	uriC                      = "mem://c.extension.yaml"
)

func mustResolve(t *testing.T, docs map[string][]byte, root string) *ResolvedGraph {
	t.Helper()
	return mustResolveLocations(t, docs, nil, root)
}

func mustResolveLocations(t *testing.T, docs map[string][]byte, locations map[string]string, root string) *ResolvedGraph {
	t.Helper()
	r := NewResolver(NewInMemoryLoader(docs))
	r.Locations = locations
	g, err := r.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve(%s): unexpected error: %v", root, err)
	}
	return g
}

func TestResolve_SingleExtensionNoDependencies(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + uriA + `
spec:
  kinds:
    - name: widget
`),
	}
	g := mustResolve(t, docs, uriA)
	if len(g.Extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(g.Extensions))
	}
	if g.ByID[uriA] == nil {
		t.Fatalf("expected %s in ByID", uriA)
	}
}

func TestResolve_TransitiveDependenciesInTopologicalOrder(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata:
  id: ` + uriA + `
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: a}]
`),
		uriB: []byte(`
metadata:
  id: ` + uriB + `
spec:
  dependencies: [` + uriC + `]
  kinds: [{name: b}]
`),
		uriC: []byte(`
metadata:
  id: ` + uriC + `
spec:
  kinds: [{name: c}]
`),
	}
	g := mustResolve(t, docs, uriA)
	if len(g.Extensions) != 3 {
		t.Fatalf("expected 3 extensions, got %d", len(g.Extensions))
	}
	// dependency-first: c before b before a
	order := map[string]int{}
	for i, ext := range g.Extensions {
		order[ext.Metadata.ID] = i
	}
	if !(order[uriC] < order[uriB] && order[uriB] < order[uriA]) {
		t.Fatalf("expected topological order c,b,a; got order %v", order)
	}
}

func TestResolve_DiamondDependencyFetchedOnce(t *testing.T) {
	// a depends on b and c, both of which depend on d.
	uriD := "mem://d.extension.yaml"
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `, ` + uriC + `]
  kinds: [{name: a}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  dependencies: [` + uriD + `]
  kinds: [{name: b}]
`),
		uriC: []byte(`
metadata: {id: ` + uriC + `}
spec:
  dependencies: [` + uriD + `]
  kinds: [{name: c}]
`),
		uriD: []byte(`
metadata: {id: ` + uriD + `}
spec:
  kinds: [{name: d}]
`),
	}
	g := mustResolve(t, docs, uriA)
	if len(g.Extensions) != 4 {
		t.Fatalf("expected 4 unique extensions, got %d: %v", len(g.Extensions), g.Extensions)
	}
}

func TestResolve_BrokenReferenceFails(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: a}]
`),
		// uriB deliberately missing.
	}
	r := NewResolver(NewInMemoryLoader(docs))
	_, err := r.Resolve(uriA)
	if err == nil {
		t.Fatal("expected error for broken reference, got nil")
	}
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("expected *FetchError, got %T: %v", err, err)
	}
	if fetchErr.URI != uriB {
		t.Fatalf("expected fetch error for %s, got %s", uriB, fetchErr.URI)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected error to wrap ErrNotFound, got %v", err)
	}
}

func TestResolve_DirectCycleFails(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  dependencies: [` + uriA + `]
`),
	}
	r := NewResolver(NewInMemoryLoader(docs))
	_, err := r.Resolve(uriA)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	var cycleErr *CycleError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected *CycleError, got %T: %v", err, err)
	}
}

func TestResolve_SelfCycleFails(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriA + `]
`),
	}
	r := NewResolver(NewInMemoryLoader(docs))
	_, err := r.Resolve(uriA)
	var cycleErr *CycleError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected *CycleError, got %T: %v", err, err)
	}
}

func TestResolve_MismatchedMetadataIDFails(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriB + `}
spec: {}
`),
	}
	r := NewResolver(NewInMemoryLoader(docs))
	_, err := r.Resolve(uriA)
	if err == nil {
		t.Fatal("expected error for mismatched metadata.id, got nil")
	}
}

// A URI whose extension depends on a URI whose extension depends on a URI
// whose extension depends on a URI, four levels deep, with no dependency
// declared at each level but the one right below it - the resolver
// shouldn't need to know the chain's depth up front.
func TestResolve_FourLevelNestedChain(t *testing.T) {
	uriD := "mem://d.extension.yaml"
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: kindA}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  dependencies: [` + uriC + `]
  kinds: [{name: kindB}]
`),
		uriC: []byte(`
metadata: {id: ` + uriC + `}
spec:
  dependencies: [` + uriD + `]
  kinds: [{name: kindC}]
`),
		uriD: []byte(`
metadata: {id: ` + uriD + `}
spec:
  kinds: [{name: kindD}]
`),
	}

	g := mustResolve(t, docs, uriA)
	if len(g.Extensions) != 4 {
		t.Fatalf("expected 4 extensions, got %d: %v", len(g.Extensions), g.Extensions)
	}

	order := map[string]int{}
	for i, ext := range g.Extensions {
		order[ext.Metadata.ID] = i
	}
	if !(order[uriD] < order[uriC] && order[uriC] < order[uriB] && order[uriB] < order[uriA]) {
		t.Fatalf("expected topological order d,c,b,a; got order %v", order)
	}

	c, err := Merge(g)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	for _, kind := range []string{"kindA", "kindB", "kindC", "kindD"} {
		if !c.IsValidKind(kind) {
			t.Errorf("expected kind %q from the resolved chain to be valid", kind)
		}
	}
}

func TestResolve_MappedIdentifierPreservesIdentity(t *testing.T) {
	const id = "mem://a/"
	const fullURI = "mem://a/" + standardExtensionFilename
	docs := map[string][]byte{
		fullURI: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + id + `
spec:
  kinds:
    - name: widget
`),
	}

	g := mustResolveLocations(t, docs, map[string]string{id: fullURI}, id)
	if len(g.Extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(g.Extensions))
	}
	if g.ByID[id] == nil || g.ByID[fullURI] != nil {
		t.Fatalf("expected only requested identifier %s in ByID, got %v", id, g.ByID)
	}
}

func TestResolve_MappedDependencyPreservesIdentity(t *testing.T) {
	const rootURI = "mem://root/" + standardExtensionFilename
	const depID = "mem://dep/"
	const depFullURI = "mem://dep/" + standardExtensionFilename
	docs := map[string][]byte{
		rootURI: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + rootURI + `
spec:
  dependencies:
    - mem://dep/
  kinds:
    - name: root-kind
`),
		depFullURI: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + depID + `
spec:
  kinds:
    - name: dep-kind
`),
	}

	g := mustResolveLocations(t, docs, map[string]string{depID: depFullURI}, rootURI)
	if len(g.Extensions) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(g.Extensions))
	}
	if g.ByID[depID] == nil || g.ByID[depFullURI] != nil {
		t.Fatalf("expected dependency keyed under %s, got %v", depID, g.ByID)
	}
}

func TestResolve_NonStandardFilenameURIIsLeftAlone(t *testing.T) {
	// A complete identifier that doesn't use the standard filename (and
	// doesn't end in "/") is not shorthand - it's used exactly as given.
	g := mustResolve(t, map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  kinds: [{name: widget}]
`),
	}, uriA)
	if g.ByID[uriA] == nil {
		t.Fatalf("expected extension resolved and keyed under the original URI %s", uriA)
	}
}

func TestResolve_CanonicalReferenceUsesMappedLocation(t *testing.T) {
	const id = "https://runtimeconditions.io/extensions/aws/s3:0.1.0"
	const fullURI = "https://example.test/extensions/aws-s3/0.1.0/" + standardExtensionFilename
	docs := map[string][]byte{
		fullURI: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + id + `
  version: 0.1.0
spec:
  kinds:
    - name: widget
`),
	}

	g := mustResolveLocations(t, docs, map[string]string{id: fullURI}, id)
	if len(g.Extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(g.Extensions))
	}
	if g.ByID[id] == nil || g.ByID[fullURI] != nil {
		t.Fatalf("expected canonical identifier %s in ByID, got %v", id, g.ByID)
	}
	catalog, err := Merge(g)
	if err != nil {
		t.Fatal(err)
	}
	if owner, _, err := catalog.resolveKind("widget", id); err != nil || owner != id {
		t.Fatalf("canonical kind owner = %q, error = %v", owner, err)
	}
}

func TestResolve_ColonVersionNonSemverVersionWorks(t *testing.T) {
	// The version segment is an arbitrary string, not constrained to
	// semver - v1alpha1 must resolve the same way 0.1.0 does.
	const id = "https://runtimeconditions.io/extensions/common-integrations:v1alpha1"
	const fullURI = "https://example.test/extensions/common-integrations/v1alpha1/" + standardExtensionFilename
	docs := map[string][]byte{
		fullURI: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + id + `
spec:
  kinds:
    - name: api
`),
	}

	g := mustResolveLocations(t, docs, map[string]string{id: fullURI}, id)
	if g.ByID[id] == nil {
		t.Fatalf("expected canonical identifier %s in ByID", id)
	}
}

func TestResolve_MappedLocationDoesNotOverrideDefinitionIdentity(t *testing.T) {
	const fullURI = "https://example.test/extensions/common-integrations/v1alpha1/" + standardExtensionFilename
	docs := map[string][]byte{
		fullURI: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + fullURI + `
spec:
  kinds:
    - name: api
`),
	}

	for _, id := range []string{"https://example.test/extensions/common-integrations:v1alpha1", "https://example.test/extensions/rc/common-integrations:v1alpha1", ""} {
		r := NewResolver(NewInMemoryLoader(docs))
		r.Locations = map[string]string{id: fullURI}
		if _, err := r.Resolve(id); err == nil {
			t.Errorf("accepted identifier %q for definition %q", id, fullURI)
		}
	}
}

func TestResolve_UnmappedIdentifiersAreLoadedVerbatim(t *testing.T) {
	for _, id := range []string{"https://runtimeconditions.io/extensions/common-integrations:v1alpha1", "https://example.test:8443/extension.yaml", "example.opaque:1", "mem://extension/", "https://example.test/extensions/rc/common:v1"} {
		t.Run(id, func(t *testing.T) {
			g := mustResolve(t, map[string][]byte{id: []byte("metadata: {id: " + id + "}\n")}, id)
			if g.ByID[id] == nil {
				t.Fatalf("requested identifier %q was changed", id)
			}
		})
	}
}

func TestResolve_MappedCanonicalDependencyGraph(t *testing.T) {
	const root = "https://runtimeconditions.io/extensions/root:1"
	const a = "https://runtimeconditions.io/extensions/shared:1"
	const b = "https://runtimeconditions.io/extensions/shared:2"
	const leaf = "https://runtimeconditions.io/extensions/leaf:1"
	locations := map[string]string{root: "mem://root", a: "mem://a", b: "mem://b", leaf: "mem://leaf"}
	docs := map[string][]byte{
		locations[root]: []byte("metadata: {id: " + root + "}\nspec: {dependencies: [" + a + ", " + b + "]}\n"),
		locations[a]:    []byte("metadata: {id: " + a + "}\nspec: {dependencies: [" + leaf + "]}\n"),
		locations[b]:    []byte("metadata: {id: " + b + "}\nspec: {dependencies: [" + leaf + "]}\n"),
		locations[leaf]: []byte("metadata: {id: " + leaf + "}\n"),
	}
	loads := map[string]int{}
	loader := NewInMemoryLoader(docs)
	r := NewResolver(func(location string) (io.ReadCloser, error) {
		loads[location]++
		return loader(location)
	})
	r.Locations = locations
	g, err := r.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.ByID) != 4 || len(g.Extensions) != 4 {
		t.Fatalf("expected four distinct identities, got %v", g.ByID)
	}
	for i, id := range []string{leaf, a, b, root} {
		if g.ByID[id] == nil || g.Extensions[i].Metadata.ID != id || loads[locations[id]] != 1 {
			t.Fatalf("identity %q was lost, reordered, or fetched more than once", id)
		}
	}
	// Sharing a retrieval location must not deduplicate distinct identities.
	locations[b] = locations[a]
	if _, err := r.Resolve(root); err == nil {
		t.Fatal("accepted two identities for one definition")
	}
	locations[b] = "mem://b"
	docs[locations[leaf]] = []byte("metadata: {id: " + leaf + "}\nspec: {dependencies: [" + root + "]}\n")
	_, err = r.Resolve(root)
	var cycle *CycleError
	if !errors.As(err, &cycle) || !reflect.DeepEqual(cycle.Path, []string{root, a, leaf, root}) {
		t.Fatalf("expected cycle path with canonical identifiers, got %v", err)
	}
}

func TestResolve_MappingFailures(t *testing.T) {
	const id = "https://runtimeconditions.io/extensions/example:1"
	for _, definitionID := range []string{"", "https://runtimeconditions.io/extensions/example:2"} {
		r := NewResolver(NewInMemoryLoader(map[string][]byte{"mem://definition": []byte("metadata: {id: '" + definitionID + "'}\n")}))
		r.Locations = map[string]string{id: "mem://definition"}
		if _, err := r.Resolve(id); err == nil {
			t.Errorf("accepted mismatched or missing definition ID %q", definitionID)
		}
	}
	r := NewResolver(NewInMemoryLoader(nil))
	r.Locations = map[string]string{id: "mem://missing"}
	_, err := r.Resolve(id)
	var fetch *FetchError
	if !errors.As(err, &fetch) || fetch.URI != "mem://missing" || !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected fetch error at mapped location, got %v", err)
	}
	r.Loader = func(location string) (io.ReadCloser, error) {
		t.Fatalf("loader called with empty retrieval mapping: %q", location)
		return nil, nil
	}
	r.Locations[id] = ""
	if _, err := r.Resolve(id); err == nil {
		t.Fatal("accepted an empty retrieval mapping")
	}
}
