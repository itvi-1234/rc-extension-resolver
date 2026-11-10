package resolver

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

const (
	standardExtensionFilename = "runtimeconditions.extension.yaml"
	uriA                      = "https://example.test/extensions/a:1"
	uriB                      = "https://example.test/extensions/b:1"
	uriC                      = "https://example.test/extensions/c:1"
)

func mustResolve(t *testing.T, docs map[string][]byte, root string) *ResolvedGraph {
	t.Helper()
	locations := make(map[string]string, len(docs))
	for id := range docs {
		locations[id] = id
	}
	return mustResolveLocations(t, docs, locations, root)
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
	uriD := "https://example.test/extensions/d:1"
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
	r.Locations = map[string]string{uriA: uriA, uriB: uriB}
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
	r.Locations = map[string]string{uriA: uriA, uriB: uriB}
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
	r.Locations = map[string]string{uriA: uriA}
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
	r.Locations = map[string]string{uriA: uriA}
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
	uriD := "https://example.test/extensions/d:1"
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
	const id = uriA
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
	const rootURI = "https://example.test/extensions/root:1"
	const depID = "https://example.test/extensions/dependency:1"
	const depFullURI = "mem://dep/" + standardExtensionFilename
	docs := map[string][]byte{
		rootURI: []byte(`
apiVersion: runtimeconditions.io/v1alpha1
kind: RuntimeConditionsExtensionDefinition
metadata:
  id: ` + rootURI + `
spec:
  dependencies:
    - ` + depID + `
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

	g := mustResolveLocations(t, docs, map[string]string{rootURI: rootURI, depID: depFullURI}, rootURI)
	if len(g.Extensions) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(g.Extensions))
	}
	if g.ByID[depID] == nil || g.ByID[depFullURI] != nil {
		t.Fatalf("expected dependency keyed under %s, got %v", depID, g.ByID)
	}
}

func TestResolve_ExplicitMappingKeepsReference(t *testing.T) {
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

func TestResolve_VersionedIdentifiersKeepExactSpelling(t *testing.T) {
	for _, id := range []string{"https://runtimeconditions.io/extensions/common-integrations:v1alpha1", "https://EXAMPLE.test:8443/CaseSensitive/%73ervice:V1+build", "https://example.test/service:sha256:abc", "urn:service:1.0.0", "custom+resolver:service:1.0.0", "file:///opt/extensions/service:1.0.0", "oci://example.test/extensions:1.0.0"} {
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
	delete(r.Locations, id)
	if _, err := r.Resolve(id); err == nil || !strings.Contains(err.Error(), "no retrieval mapping") {
		t.Fatalf("expected a missing retrieval mapping error, got %v", err)
	}
}

func TestResolve_RejectsUnsupportedReferencesBeforeLoading(t *testing.T) {
	for _, id := range []string{
		"", "relative/path:1", "/absolute/path:1", "//example.test/path:1", "1bad:service:1",
		"https://example.test/service", "https://example.test:8443", "urn:release-a", "custom+resolver:release-a",
		"https://example.test/service:", "https://example.test/:1", "https://example.test/service:.", "https://example.test/service:..",
		"https:///service:1", "https://user@example.test/service:1", "https://example.test/%invalid:1",
		"https://example.test/service:bad version", "https://example.test/service:bad\u0085version", "https://example.test/service:bad\\version",
		"https://example.test/service:1?revision=abc", "https://example.test/service:1#fragment",
		"file:///opt/extensions/release.yaml", "oci://example.test/extensions",
		"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
		"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml:v1alpha1",
		"https://example.test/definition.yml:1", "https://example.test/definition.json:1", "https://example.test/definition%2Eyaml:1",
	} {
		for _, test := range []struct {
			name, root string
			loads      int
		}{{"root", id, 0}, {"dependency", uriA, 1}} {
			t.Run(test.name+"/"+id, func(t *testing.T) {
				loads := 0
				r := NewResolver(func(location string) (io.ReadCloser, error) {
					loads++
					if location != "mem://root" {
						t.Fatalf("unsupported reference reached loader at %q", location)
					}
					return io.NopCloser(strings.NewReader(fmt.Sprintf("metadata: {id: %q}\nspec: {dependencies: [%q]}\n", uriA, id))), nil
				})
				r.Locations = map[string]string{uriA: "mem://root", id: "mem://invalid"}
				graph, err := r.Resolve(test.root)
				if err == nil || !strings.Contains(err.Error(), "invalid extension reference") || graph != nil || loads != test.loads {
					t.Fatalf("expected reference rejection after %d loads, got graph=%v loads=%d error=%v", test.loads, graph, loads, err)
				}
			})
		}
	}
}

func TestResolve_VersionAnnotations(t *testing.T) {
	for _, test := range []struct {
		name, version, versionYAML string
		valid                      bool
	}{
		{"omitted", "1.0.0", "", true}, {"matching", "1.0.0", "  version: 1.0.0\n", true},
		{"label", "v1alpha1", "  version: v1alpha1\n", true}, {"digest", "sha256:abc", "  version: sha256:abc\n", true},
		{"quoted number", "1", "  version: '1'\n", true},
		{"alias", "1.0.0", "  release: &release 1.0.0\n  version: *release\n", true},
		{"inherited", "1.0.0", "  <<: {version: 1.0.0}\n", true},
		{"mismatch", "1.0.0", "  version: 2.0.0\n", false}, {"case mismatch", "v1alpha1", "  version: V1alpha1\n", false},
		{"empty", "1.0.0", "  version: ''\n", false}, {"null", "1.0.0", "  version: null\n", false},
		{"number", "1", "  version: 1\n", false}, {"boolean", "true", "  version: true\n", false},
		{"sequence", "1.0.0", "  version: [1.0.0]\n", false}, {"mapping", "1.0.0", "  version: {value: 1.0.0}\n", false},
		{"null alias", "1.0.0", "  release: &release null\n  version: *release\n", false},
		{"inherited null", "1.0.0", "  <<: {version: null}\n", false},
		{"inherited empty", "1.0.0", "  <<: {version: ''}\n", false},
	} {
		for _, role := range []string{"root", "dependency"} {
			t.Run(role+"/"+test.name, func(t *testing.T) {
				id := "https://example.test/extensions/example:" + test.version
				docs := map[string][]byte{"mem://definition.yaml": []byte("metadata:\n  id: " + id + "\n" + test.versionYAML)}
				r := NewResolver(NewInMemoryLoader(docs))
				r.Locations = map[string]string{id: "mem://definition.yaml"}
				root, count := id, 1
				if role == "dependency" {
					root, count = uriA, 2
					r.Locations[uriA] = "mem://root.yaml"
					docs["mem://root.yaml"] = []byte("metadata: {id: " + uriA + "}\nspec: {dependencies: [" + id + "]}\n")
				}
				graph, err := r.Resolve(root)
				if !test.valid {
					if err == nil || graph != nil || !strings.Contains(err.Error(), "metadata.version") {
						t.Fatalf("expected version annotation rejection, got %v", err)
					}
					return
				}
				expectedVersion := test.version
				if test.versionYAML == "" {
					expectedVersion = ""
				}
				if err != nil || len(graph.ByID) != count || graph.ByID[id] == nil || graph.ByID[id].Metadata.Version != expectedVersion {
					t.Fatalf("version annotation changed identity or was not preserved: %v", err)
				}
			})
		}
	}
}

func TestResolve_UnmappedDependencyDoesNotReachLoader(t *testing.T) {
	r := NewResolver(func(location string) (io.ReadCloser, error) {
		if location != "mem://root.yaml" {
			t.Fatalf("unmapped dependency reached loader at %q", location)
		}
		return io.NopCloser(strings.NewReader("metadata: {id: " + uriA + "}\nspec: {dependencies: [" + uriB + "]}\n")), nil
	})
	r.Locations = map[string]string{uriA: "mem://root.yaml"}
	if _, err := r.Resolve(uriA); err == nil || !strings.Contains(err.Error(), "no retrieval mapping") {
		t.Fatalf("expected a missing dependency mapping error, got %v", err)
	}
}
