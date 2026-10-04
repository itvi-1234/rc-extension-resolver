package resolver

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

const standardExtensionFilename = "runtimeconditions.extension.yaml"

const defaultCatalogNamespaceMarker = "/extensions/rc/"

func normalizeExtensionURI(uri string) string {
	if idx := strings.LastIndex(uri, ":"); idx > 0 {
		base, version := uri[:idx], uri[idx+1:]
		if version != "" && isAbsoluteHTTPURI(base) {
			return stripDefaultNamespace(strings.TrimSuffix(base, "/")) + "/" + version + "/" + standardExtensionFilename
		}
	}
	if strings.HasSuffix(uri, "/") {
		return stripDefaultNamespace(strings.TrimSuffix(uri, "/")) + "/" + standardExtensionFilename
	}
	return uri
}

func isAbsoluteHTTPURI(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func stripDefaultNamespace(uri string) string {
	if idx := strings.Index(uri, defaultCatalogNamespaceMarker); idx != -1 {
		return uri[:idx] + "/extensions/" + uri[idx+len(defaultCatalogNamespaceMarker):]
	}
	return uri
}

// visitState is the three-color DFS scheme: visiting-but-not-visited
// means we've looped back to an ancestor, i.e. a cycle.
type visitState int

const (
	unvisited visitState = iota
	visiting
	visited
)

type Resolver struct {
	Loader LoaderFunc
}

func NewResolver(loader LoaderFunc) *Resolver {
	return &Resolver{Loader: loader}
}

// ResolvedGraph is every extension reachable from a root, deduplicated by
// identifier URI, in dependency-first order.
type ResolvedGraph struct {
	Extensions []*ExtensionDefinition
	ByID       map[string]*ExtensionDefinition
}

func (r *Resolver) Resolve(rootURI string) (*ResolvedGraph, error) {
	g := &ResolvedGraph{ByID: make(map[string]*ExtensionDefinition)}
	state := make(map[string]visitState)

	var visit func(uri string, path []string) error
	visit = func(uri string, path []string) error {
		uri = normalizeExtensionURI(uri)
		switch state[uri] {
		case visiting:
			return &CycleError{Path: append(append([]string{}, path...), uri)}
		case visited:
			return nil
		}
		state[uri] = visiting

		ext, err := r.fetch(uri)
		if err != nil {
			return err
		}
		if ext.Metadata.ID != "" && ext.Metadata.ID != uri {
			return fmt.Errorf("fetch %s: document declares metadata.id %q, expected %q", uri, ext.Metadata.ID, uri)
		}

		nextPath := append(append([]string{}, path...), uri)
		for _, dep := range ext.Spec.Dependencies {
			if err := visit(dep, nextPath); err != nil {
				return err
			}
		}

		state[uri] = visited
		g.ByID[uri] = ext
		g.Extensions = append(g.Extensions, ext)
		return nil
	}

	if err := visit(rootURI, nil); err != nil {
		return nil, err
	}
	return g, nil
}

func (r *Resolver) fetch(uri string) (*ExtensionDefinition, error) {
	body, err := r.Loader(uri)
	if err != nil {
		return nil, &FetchError{URI: uri, Err: err}
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, &FetchError{URI: uri, Err: err}
	}

	var ext ExtensionDefinition
	if err := yaml.Unmarshal(data, &ext); err != nil {
		return nil, &FetchError{URI: uri, Err: fmt.Errorf("parsing document: %w", err)}
	}
	return &ext, nil
}
