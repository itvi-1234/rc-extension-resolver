//go:build live

package resolver

import (
	"io"
	"testing"
	"time"
)

// Verify automatic lookup against the published catalog without explicit mappings.
func TestResolve_LiveOfficialCatalog(t *testing.T) {
	for _, test := range []struct{ reference, artifact, version string }{
		{"https://runtimeconditions.io/extensions/aws/s3:0.2.0", "https://runtimeconditions.io/extensions/aws/s3/0.2.0/runtimeconditions.extension.yaml", "0.2.0"},
		{"https://runtimeconditions.io/extensions/common-integrations:v1alpha1", "https://runtimeconditions.io/extensions/common-integrations/v1alpha1/runtimeconditions.extension.yaml", "v1alpha1"},
		{"https://runtimeconditions.io/extensions/env-configuration:v1alpha1", "https://runtimeconditions.io/extensions/env-configuration/v1alpha1/runtimeconditions.extension.yaml", "v1alpha1"},
	} {
		t.Run(test.reference, func(t *testing.T) {
			loader := NewHTTPLoader(10 * time.Second)
			var firstURL string
			r := NewResolver(func(location string) (io.ReadCloser, error) {
				if firstURL == "" {
					firstURL = location
				}
				return loader(location)
			})
			g, err := r.Resolve(test.reference)
			if err != nil {
				t.Fatalf("Resolve(%s): %v", test.reference, err)
			}
			if firstURL != test.artifact {
				t.Fatalf("retrieved %q, expected %q", firstURL, test.artifact)
			}
			definition := g.ByID[test.reference]
			if definition == nil {
				t.Fatalf("expected exact reference %q in the graph", test.reference)
			}
			if definition.Metadata.Version != "" && definition.Metadata.Version != test.version {
				t.Fatalf("definition version %q differs from requested %q", definition.Metadata.Version, test.version)
			}
		})
	}
}
