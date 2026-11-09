package resolver

import (
	"fmt"
	"io"

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

	// Locations maps exact extension identifiers to retrieval locations. An
	// identifier without a mapping is passed directly to Loader.
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
		if id == "" {
			return fmt.Errorf("extension identifier must not be empty")
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
			location = id
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
