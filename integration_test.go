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

// catalogResolverFor replaces HTTP transport with unchanged real catalog files.
// The resolver still selects the artifact URL from each requested reference.
func catalogResolverFor(t *testing.T, root string) *Resolver {
	t.Helper()
	urlToPath := map[string]string{
		"https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha1/runtimeconditions.extension.yaml": "catalog/rc/common-integrations/common-integrations-v1alpha1.yaml",
		"https://runtimeconditions.io/extensions/rc/env-configuration/v1alpha1/runtimeconditions.extension.yaml":   "catalog/rc/env-configuration/env-configuration-v1alpha1.yaml",
		"https://runtimeconditions.io/extensions/aws/s3/0.1.0/runtimeconditions.extension.yaml":                 "catalog/aws/s3/releases/0.1.0/runtimeconditions.extension.yaml",
	}
	docs := make(map[string][]byte)
	for artifactURL, relPath := range urlToPath {
		path := filepath.Join(root, relPath)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("extensions repo not available at %s (%v); skipping integration test", root, err)
		}
		docs[artifactURL] = data
	}
	return NewResolver(NewInMemoryLoader(docs))
}

func TestIntegration_ResolveAndMergeRealExtensions(t *testing.T) {
	root := extensionsRepoRoot(t)
	r := catalogResolverFor(t, root)
	g, err := r.Resolve("https://runtimeconditions.io/extensions/rc/env-configuration:v1alpha1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// env-configuration depends on common-integrations, so both should be present.
	if len(g.Extensions) != 2 {
		t.Fatalf("expected 2 extensions (env-configuration + common-integrations), got %d", len(g.Extensions))
	}
	for _, id := range []string{"https://runtimeconditions.io/extensions/rc/env-configuration:v1alpha1", "https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha1/runtimeconditions.extension.yaml"} {
		if g.ByID[id] == nil {
			t.Fatalf("requested reference %q missing from graph", id)
		}
	}
	definition := g.ByID["https://runtimeconditions.io/extensions/rc/env-configuration:v1alpha1"]
	if definition.Metadata.ID != "https://runtimeconditions.io/extensions/rc/env-configuration/v1alpha1/runtimeconditions.extension.yaml" || definition.Metadata.Version != "v1alpha1" {
		t.Fatal("resolution changed the definition's separate identity or version")
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
	r := catalogResolverFor(t, root)
	g, err := r.Resolve("https://runtimeconditions.io/extensions/aws/s3:0.1.0")
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
	if owner, _, err := c.resolveKind("aws.s3", "https://runtimeconditions.io/extensions/aws/s3:0.1.0"); err != nil || owner != "https://runtimeconditions.io/extensions/aws/s3:0.1.0" {
		t.Fatalf("profile reference did not retain ownership: %q, %v", owner, err)
	}
}
