package resolver

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

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

	// Locations optionally maps exact references to retrieval locations. Without
	// a mapping, official catalog references are expanded; other inputs reach Loader unchanged.
	Locations map[string]string
}

func NewResolver(loader LoaderFunc) *Resolver {
	return &Resolver{Loader: loader}
}

// ResolvedGraph is every extension reachable from a root, deduplicated by
// exact extension identifier, in dependency-first order.
type ResolvedGraph struct {
	Extensions []*ExtensionDefinition
	ByID       map[string]*ExtensionDefinition
}

// Resolve preserves exact references independently of definition metadata.
// Locations overrides automatic catalog lookup and direct loading.
func (r *Resolver) Resolve(rootID string) (*ResolvedGraph, error) {
	return r.ResolveAll(rootID)
}

// ResolveAll resolves several roots, such as every item of a profile's
// extensions, into one graph. Every reference keeps its exact spelling as a
// graph key, and a reference reached from more than one root is fetched once.
// Two spellings of the same file stay two references that share a Location.
func (r *Resolver) ResolveAll(rootIDs ...string) (*ResolvedGraph, error) {
	g := &ResolvedGraph{ByID: make(map[string]*ExtensionDefinition)}
	state := make(map[string]visitState)

	var visit func(id string, path []string) error
	visit = func(id string, path []string) error {
		if id == "" {
			return fmt.Errorf("extension reference must not be empty")
		}
		switch state[id] {
		case visiting:
			return &CycleError{Path: append(append([]string{}, path...), id)}
		case visited:
			return nil
		}
		state[id] = visiting

		catalogURL, version := officialCatalogLocation(id)
		location, mapped := r.Locations[id]
		if !mapped {
			location = catalogURL
		}
		if location == "" {
			return fmt.Errorf("extension %q has an empty retrieval location", id)
		}
		ext, err := r.fetch(location)
		if err != nil {
			return err
		}
		if ext.Metadata.ID == "" {
			return fmt.Errorf("fetch %s: metadata.id must be a non-empty string", location)
		}
		if version != "" && ext.Metadata.Version != "" && ext.Metadata.Version != version {
			return fmt.Errorf("fetch %s: metadata.version %q must exactly match reference version %q", location, ext.Metadata.Version, version)
		}

		nextPath := append(append([]string{}, path...), id)
		for _, dep := range ext.Spec.Dependencies {
			if err := visit(dep, nextPath); err != nil {
				return err
			}
		}

		state[id] = visited
		g.ByID[id] = ext
		g.Extensions = append(g.Extensions, ext)
		return nil
	}

	for _, rootID := range rootIDs {
		if err := visit(rootID, nil); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// officialCatalogLocation expands <url>:<version> only for HTTPS URLs on
// runtimeconditions.io with a non-empty location and version. Other references
// retain their exact spelling for the configured loader or mapping.
func officialCatalogLocation(reference string) (string, string) {
	parsed, err := url.Parse(reference)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "runtimeconditions.io") || parsed.User != nil || strings.ContainsAny(reference, "?#\\") || strings.ContainsFunc(reference, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return reference, ""
	}
	component := reference[strings.LastIndex(reference, "/")+1:]
	name, version, versioned := strings.Cut(component, ":")
	// A version of "." or ".." would turn the expanded location into a path that
	// leaves the extension's directory.
	if parsed.Path == "" || !versioned || name == "" || version == "" || version == "." || version == ".." {
		return reference, ""
	}
	base := reference[:len(reference)-len(version)-1]
	return base + "/" + url.PathEscape(version) + "/runtimeconditions.extension.yaml", version
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
	ext.Location = uri
	return &ext, nil
}
