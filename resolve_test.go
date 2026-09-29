package resolver

import (
	"errors"
	"testing"
)

const (
	uriA = "mem://a.extension.yaml"
	uriB = "mem://b.extension.yaml"
	uriC = "mem://c.extension.yaml"
)

func mustResolve(t *testing.T, docs map[string][]byte, root string) *ResolvedGraph {
	t.Helper()
	r := NewResolver(NewInMemoryLoader(docs))
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
