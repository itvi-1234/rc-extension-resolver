package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

// extensionsRepoRoot points at the sibling extensions checkout this repo
// resolves against for the integration test. Overridable via
// RC_EXTENSIONS_REPO for CI layouts that don't check it out as a sibling.
func extensionsRepoRoot(t *testing.T) string {
	t.Helper()
	if root := os.Getenv("RC_EXTENSIONS_REPO"); root != "" {
		return root
	}
	return filepath.Join("..", "extensions")
}

// fileLoaderFor maps published identifier URIs to their file on disk in a
// checkout of the extensions repo, per the table in that repo's README.
// This is the resolver's real fetch path with the network swapped for a
// local checkout - no fake documents, no mocked schema shapes.
func fileLoaderFor(t *testing.T, root string) LoaderFunc {
	t.Helper()
	uriToPath := map[string]string{
		"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml": "catalog/rc/common-integrations/common-integrations-v1alpha1.yaml",
		"https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml":   "catalog/rc/env-configuration/env-configuration-v1alpha1.yaml",
		"https://runtimeconditions.io/extensions/aws-s3/0.1.0/runtimeconditions.extension.yaml":                 "catalog/aws/s3/releases/0.1.0/runtimeconditions.extension.yaml",
	}
	docs := make(map[string][]byte)
	for uri, relPath := range uriToPath {
		data, err := os.ReadFile(filepath.Join(root, relPath))
		if err != nil {
			t.Skipf("extensions repo not available at %s (%v); skipping integration test", root, err)
		}
		docs[uri] = data
	}
	return NewInMemoryLoader(docs)
}

func TestIntegration_ResolveAndMergeRealExtensions(t *testing.T) {
	root := extensionsRepoRoot(t)
	loader := fileLoaderFor(t, root)

	r := NewResolver(loader)
	g, err := r.Resolve("https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// env-configuration depends on common-integrations, so both should be present.
	if len(g.Extensions) != 2 {
		t.Fatalf("expected 2 extensions (env-configuration + common-integrations), got %d", len(g.Extensions))
	}

	c, err := Merge(g)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	for _, kind := range []string{"api", "datastore", "cache"} {
		if !c.IsValidKind(kind) {
			t.Errorf("expected kind %q from common-integrations to be valid", kind)
		}
	}
	if !c.IsValidInterfaceType("api", "http") {
		t.Error("expected api/http interface type to be valid")
	}

	result, err := c.ValidateCondition(map[string]any{
		"kind": "api",
		"interface": map[string]any{
			"type": "http",
			"spec": map[string]any{
				"format": "openapi",
				"uri":    "https://example.com/openapi.yaml",
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateCondition: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid condition against real common-integrations schema, got errors: %v", result.Errors)
	}
}

func TestIntegration_ResolveIndependentExtensionNoConflict(t *testing.T) {
	root := extensionsRepoRoot(t)
	loader := fileLoaderFor(t, root)

	r := NewResolver(loader)
	g, err := r.Resolve("https://runtimeconditions.io/extensions/aws-s3/0.1.0/runtimeconditions.extension.yaml")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	c, err := Merge(g)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !c.IsValidKind("aws.s3") {
		t.Error("expected kind aws.s3 to be valid")
	}
}
