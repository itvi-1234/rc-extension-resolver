//go:build live

package resolver

import (
	"testing"
	"time"
)

// Canonical references and separate retrieval mappings are covered by
// in-memory tests until canonical definitions are published.
func TestResolve_LiveCatalogArtifactURLs(t *testing.T) {
	for _, id := range []string{
		"https://runtimeconditions.io/extensions/aws/s3/0.2.0/runtimeconditions.extension.yaml",
		"https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml",
		"https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml",
	} {
		t.Run(id, func(t *testing.T) {
			r := NewResolver(NewHTTPLoader(10 * time.Second))
			g, err := r.Resolve(id)
			if err != nil {
				t.Fatalf("Resolve(%s): %v", id, err)
			}
			if g.ByID[id] == nil {
				t.Fatalf("expected %s in the resolved graph", id)
			}
		})
	}
}
