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

	// Locations maps complete versioned extension references to retrieval
	// locations. Every requested reference requires an explicit mapping.
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

// Resolve preserves exact identifiers; Locations controls where definitions
// are retrieved. Every definition must declare the requested metadata.id.
func (r *Resolver) Resolve(rootID string) (*ResolvedGraph, error) {
	g := &ResolvedGraph{ByID: make(map[string]*ExtensionDefinition)}
	state := make(map[string]visitState)

	var visit func(id string, path []string) error
	visit = func(id string, path []string) error {
		version, err := extensionReferenceVersion(id)
		if err != nil {
			return fmt.Errorf("invalid extension reference %q: %w", id, err)
		}
		switch state[id] {
		case visiting:
			return &CycleError{Path: append(append([]string{}, path...), id)}
		case visited:
			return nil
		}
		state[id] = visiting

		location, mapped := r.Locations[id]
		if !mapped {
			return fmt.Errorf("extension %q has no retrieval mapping", id)
		}
		if location == "" {
			return fmt.Errorf("extension %q has an empty retrieval location", id)
		}
		ext, err := r.fetch(location)
		if err != nil {
			return err
		}
		if ext.Metadata.ID != id {
			return fmt.Errorf("fetch %s: document declares metadata.id %q, expected %q", location, ext.Metadata.ID, id)
		}
		if ext.Metadata.Version != "" && ext.Metadata.Version != version {
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

	if err := visit(rootID, nil); err != nil {
		return nil, err
	}
	return g, nil
}

// extensionReferenceVersion uses the same reference grammar as the binding
// normalizer: the first literal colon in the final path component (or opaque
// URI body) separates location from version. Scheme and authority colons do not.
// Version labels may contain colons; the complete identity is never rewritten.
func extensionReferenceVersion(id string) (string, error) {
	parsed, err := url.Parse(id)
	if err != nil || !parsed.IsAbs() || strings.ContainsAny(id, "?#\\") || strings.ContainsFunc(id, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return "", fmt.Errorf("extension reference must be an absolute <location>:<version> URI without whitespace, query, or fragment")
	}
	component := parsed.Opaque
	if component == "" {
		path := parsed.EscapedPath()
		component = path[strings.LastIndex(path, "/")+1:]
	}
	location, version, versioned := strings.Cut(component, ":")
	if !versioned || location == "" || version == "" || version == "." || version == ".." || strings.Contains(version, "/") {
		return "", fmt.Errorf("extension reference must include a non-empty location and explicit version as <location>:<version>")
	}
	name, err := url.PathUnescape(location)
	if err != nil || strings.HasSuffix(strings.ToLower(name), ".yaml") || strings.HasSuffix(strings.ToLower(name), ".yml") || strings.HasSuffix(strings.ToLower(name), ".json") {
		return "", fmt.Errorf("extension reference location must identify an extension, not a definition artifact URL")
	}
	if parsed.User != nil || (strings.EqualFold(parsed.Scheme, "https") && (parsed.Hostname() == "" || parsed.Opaque != "")) {
		return "", fmt.Errorf("extension reference forbids credentials and HTTPS references require a host")
	}
	return version, nil
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
