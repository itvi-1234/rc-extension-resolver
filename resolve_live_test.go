//go:build live

package resolver

import (
	"testing"
	"time"
)

func TestResolve_LiveCatalogFullURI(t *testing.T) {
	r := NewResolver(NewHTTPLoader(10 * time.Second))
	const uri = "https://runtimeconditions.io/extensions/aws-s3/0.2.0/runtimeconditions.extension.yaml"
	g, err := r.Resolve(uri)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", uri, err)
	}
	if g.ByID[uri] == nil {
		t.Fatalf("expected %s in the resolved graph", uri)
	}
}

func TestResolve_LiveCatalogShorthandURI(t *testing.T) {
	r := NewResolver(NewHTTPLoader(10 * time.Second))
	const shorthand = "https://runtimeconditions.io/extensions/aws-s3/0.2.0/"
	const fullURI = shorthand + standardExtensionFilename
	g, err := r.Resolve(shorthand)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", shorthand, err)
	}
	if g.ByID[fullURI] == nil {
		t.Fatalf("expected shorthand %s to resolve and key under %s", shorthand, fullURI)
	}
}

func TestResolve_LiveCatalogColonVersionURI(t *testing.T) {
	r := NewResolver(NewHTTPLoader(10 * time.Second))
	const colonForm = "https://runtimeconditions.io/extensions/aws-s3:0.2.0"
	const fullURI = "https://runtimeconditions.io/extensions/aws-s3/0.2.0/" + standardExtensionFilename
	g, err := r.Resolve(colonForm)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", colonForm, err)
	}
	if g.ByID[fullURI] == nil {
		t.Fatalf("expected %s to resolve and key under %s", colonForm, fullURI)
	}
}

func TestResolve_LiveCatalogColonVersionNonSemver(t *testing.T) {
	r := NewResolver(NewHTTPLoader(10 * time.Second))
	const colonForm = "https://runtimeconditions.io/extensions/common-integrations:v1alpha1"
	const fullURI = "https://runtimeconditions.io/extensions/common-integrations/v1alpha1/" + standardExtensionFilename
	g, err := r.Resolve(colonForm)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", colonForm, err)
	}
	if g.ByID[fullURI] == nil {
		t.Fatalf("expected %s to resolve and key under %s", colonForm, fullURI)
	}
}

func TestResolve_LiveCatalogDefaultNamespacePrefixEquivalence(t *testing.T) {
	r := NewResolver(NewHTTPLoader(10 * time.Second))
	const prefixed = "https://runtimeconditions.io/extensions/rc/common-integrations:v1alpha1"
	const fullURI = "https://runtimeconditions.io/extensions/common-integrations/v1alpha1/" + standardExtensionFilename
	g, err := r.Resolve(prefixed)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", prefixed, err)
	}
	if g.ByID[fullURI] == nil {
		t.Fatalf("expected %s to resolve and key under %s", prefixed, fullURI)
	}
}
