package resolver

import (
	"fmt"
	"sync"
)

type interfaceTypeKey struct {
	kind string
	typ  string
}

type schemaEntry struct {
	def      SchemaDef
	ownerURI string
}

// Schema binding is additive, not owned: env-configuration binds a schema
// to api/http, the same scope common-integrations already owns, to
// validate interface.configuration instead of interface.spec. A Condition
// must satisfy every schema bound to its scope. Conflict is keyed on
// schema id, not on scope.
//
// kind is exempt from ownership: more than one resolved extension may
// define the same kind name. kindOwners keeps every owner in extension
// resolution order, so a Condition can disambiguate with an explicit
// extension field, or fall back to the first owner.
type Catalog struct {
	kindOwners map[string][]string
	typeOwner  map[interfaceTypeKey]string
	schemaIDs  map[string]string
	schemas    map[interfaceTypeKey][]schemaEntry
	compiled   map[string]*compiledSchema
	compileMu  sync.Mutex
}

func Merge(graph *ResolvedGraph) (*Catalog, error) {
	c := &Catalog{
		kindOwners: make(map[string][]string),
		typeOwner:  make(map[interfaceTypeKey]string),
		schemaIDs:  make(map[string]string),
		schemas:    make(map[interfaceTypeKey][]schemaEntry),
		compiled:   make(map[string]*compiledSchema),
	}

	for _, ext := range graph.Extensions {
		owner := ext.Metadata.ID

		for _, k := range ext.Spec.Kinds {
			c.kindOwners[k.Name] = append(c.kindOwners[k.Name], owner)
		}

		for _, it := range ext.Spec.InterfaceTypes {
			key := interfaceTypeKey{kind: it.TargetKind, typ: it.Name}
			if existing, ok := c.typeOwner[key]; ok && existing != owner {
				return nil, &ConflictError{Kind: it.TargetKind, InterfaceType: it.Name, DeclaredBy: []string{existing, owner}}
			}
			c.typeOwner[key] = owner
		}

		for _, s := range ext.Spec.Schemas {
			if existing, ok := c.schemaIDs[s.ID]; ok && existing != owner {
				return nil, &ConflictError{Kind: s.AppliesToKind, InterfaceType: s.AppliesToInterfaceType, DeclaredBy: []string{existing, owner}}
			}
			c.schemaIDs[s.ID] = owner

			key := interfaceTypeKey{kind: s.AppliesToKind, typ: s.AppliesToInterfaceType}
			c.schemas[key] = append(c.schemas[key], schemaEntry{def: s, ownerURI: owner})
		}
	}

	return c, nil
}

func (c *Catalog) IsValidKind(kind string) bool {
	return len(c.kindOwners[kind]) > 0
}

// resolveKind picks which extension's definition of kind a Condition uses.
// With extension set, it must be one of kind's owners. Without it, the
// first owner in extension resolution order wins, and match reports
// whether that pick was explicit or a fallback.
func (c *Catalog) resolveKind(kind, extension string) (owner string, match KindMatch, err error) {
	owners := c.kindOwners[kind]
	if len(owners) == 0 {
		return "", "", fmt.Errorf("unknown kind %q", kind)
	}
	if extension != "" {
		for _, o := range owners {
			if o == extension {
				return o, KindMatchExplicit, nil
			}
		}
		return "", "", &ExtensionMismatchError{Kind: kind, Extension: extension}
	}
	if len(owners) > 1 {
		return owners[0], KindMatchFallback, nil
	}
	return owners[0], "", nil
}

func (c *Catalog) IsValidInterfaceType(kind, interfaceType string) bool {
	_, ok := c.typeOwner[interfaceTypeKey{kind: kind, typ: interfaceType}]
	return ok
}

func (c *Catalog) schemasFor(kind, interfaceType string) []schemaEntry {
	return c.schemas[interfaceTypeKey{kind: kind, typ: interfaceType}]
}

func LoadAndMerge(loader LoaderFunc, rootURI string) (*Catalog, error) {
	graph, err := NewResolver(loader).Resolve(rootURI)
	if err != nil {
		return nil, err
	}
	return Merge(graph)
}

func (c *Catalog) String() string {
	return fmt.Sprintf("Catalog{kinds=%d, interfaceTypes=%d, schemas=%d}", len(c.kindOwners), len(c.typeOwner), len(c.schemas))
}
