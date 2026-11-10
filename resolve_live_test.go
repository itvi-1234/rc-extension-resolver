//go:build live

package resolver

import (
	"testing"
	"time"
)

// Requires canonical definitions at the publisher's release artifact URLs.
func TestResolve_LiveCanonicalCatalog(t *testing.T) {
	locations := map[string]string{
		"https://runtimeconditions.io/extensions/aws/s3:0.2.0":                 "https://runtimeconditions.io/extensions/aws/s3/releases/0.2.0/runtimeconditions.extension.yaml",
		"https://runtimeconditions.io/extensions/common-integrations:v1alpha1": "https://runtimeconditions.io/extensions/common-integrations/releases/v1alpha1/runtimeconditions.extension.yaml",
		"https://runtimeconditions.io/extensions/env-configuration:v1alpha1":   "https://runtimeconditions.io/extensions/env-configuration/releases/v1alpha1/runtimeconditions.extension.yaml",
	}
	for id := range locations {
		t.Run(id, func(t *testing.T) {
			r := NewResolver(NewHTTPLoader(10 * time.Second))
			r.Locations = locations
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
