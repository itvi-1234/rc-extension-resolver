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
type Catalog struct {
	kindOwner map[string]string
	typeOwner map[interfaceTypeKey]string
	schemaIDs map[string]string
	schemas   map[interfaceTypeKey][]schemaEntry
	compiled  map[string]*compiledSchema
	compileMu sync.Mutex
}

func Merge(graph *ResolvedGraph) (*Catalog, error) {
	c := &Catalog{
		kindOwner: make(map[string]string),
		typeOwner: make(map[interfaceTypeKey]string),
		schemaIDs: make(map[string]string),
		schemas:   make(map[interfaceTypeKey][]schemaEntry),
		compiled:  make(map[string]*compiledSchema),
	}

	for _, ext := range graph.Extensions {
		owner := ext.Metadata.ID

		for _, k := range ext.Spec.Kinds {
			if existing, ok := c.kindOwner[k.Name]; ok && existing != owner {
				return nil, &ConflictError{Kind: k.Name, DeclaredBy: []string{existing, owner}}
			}
			c.kindOwner[k.Name] = owner
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
	_, ok := c.kindOwner[kind]
	return ok
}

func (c *Catalog) IsValidInterfaceType(kind, interfaceType string) bool {
	_, ok := c.typeOwner[interfaceTypeKey{kind: kind, typ: interfaceType}]
	return ok
}

func (c *Catalog) schemasFor(kind, interfaceType string) []schemaEntry {
	return c.schemas[interfaceTypeKey{kind: kind, typ: interfaceType}]
}

func (c *Catalog) String() string {
	return fmt.Sprintf("Catalog{kinds=%d, interfaceTypes=%d, schemas=%d}", len(c.kindOwner), len(c.typeOwner), len(c.schemas))
}
