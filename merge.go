package resolver

import (
	"fmt"
	"sort"
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
// kind is exempt from ownership: extension owners should not resolve the
// same kind name, but it is still possible. kindOwners keeps every owner
// in extension resolution order, so a Condition can disambiguate with an
// explicit extension field, or fall back to the first owner.
type Catalog struct {
	kindOwners map[string][]string
	typeOwner  map[interfaceTypeKey]string
	schemaIDs  map[string]string
	// The definitions behind the owners above, so that one definition reached
	// under several references is not mistaken for several extensions.
	ownerDef  map[string]*ExtensionDefinition
	typeDef   map[interfaceTypeKey]*ExtensionDefinition
	schemaDef map[string]*ExtensionDefinition
	schemas   map[interfaceTypeKey][]schemaEntry
	compiled  map[string]*compiledSchema
	compileMu sync.Mutex
}

func Merge(graph *ResolvedGraph) (*Catalog, error) {
	c := &Catalog{
		kindOwners: make(map[string][]string),
		typeOwner:  make(map[interfaceTypeKey]string),
		schemaIDs:  make(map[string]string),
		ownerDef:   make(map[string]*ExtensionDefinition),
		typeDef:    make(map[interfaceTypeKey]*ExtensionDefinition),
		schemaDef:  make(map[string]*ExtensionDefinition),
		schemas:    make(map[interfaceTypeKey][]schemaEntry),
		compiled:   make(map[string]*compiledSchema),
	}

	references := make(map[*ExtensionDefinition][]string, len(graph.ByID))
	for reference, definition := range graph.ByID {
		references[definition] = append(references[definition], reference)
	}
	for _, ext := range graph.Extensions {
		owners := references[ext]
		if len(owners) == 0 {
			owners = []string{ext.Metadata.ID}
		}
		sort.Strings(owners)
		for _, owner := range owners {
			if err := c.mergeExtension(ext, owner); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

// definitionKey identifies a definition independently of the reference it was
// reached by: where it was retrieved from, or the object itself when the graph
// was built without a resolver.
func definitionKey(ext *ExtensionDefinition) any {
	if ext.Location != "" {
		return ext.Location
	}
	return ext
}

func sameDefinition(a, b *ExtensionDefinition) bool {
	return a != nil && b != nil && definitionKey(a) == definitionKey(b)
}

func (c *Catalog) mergeExtension(ext *ExtensionDefinition, owner string) error {
	c.ownerDef[owner] = ext

	for _, k := range ext.Spec.Kinds {
		c.kindOwners[k.Name] = append(c.kindOwners[k.Name], owner)
	}

	for _, it := range ext.Spec.InterfaceTypes {
		key := interfaceTypeKey{kind: it.TargetKind, typ: it.Name}
		if existing, ok := c.typeOwner[key]; ok && existing != owner && !sameDefinition(c.typeDef[key], ext) {
			return &ConflictError{Kind: it.TargetKind, InterfaceType: it.Name, DeclaredBy: []string{existing, owner}}
		}
		if _, ok := c.typeOwner[key]; !ok {
			c.typeOwner[key], c.typeDef[key] = owner, ext
		}
	}

	for _, s := range ext.Spec.Schemas {
		existing, ok := c.schemaIDs[s.ID]
		if ok && existing != owner {
			if !sameDefinition(c.schemaDef[s.ID], ext) {
				return &ConflictError{Kind: s.AppliesToKind, InterfaceType: s.AppliesToInterfaceType, DeclaredBy: []string{existing, owner}}
			}
			// The same definition reached under another reference: already bound.
			continue
		}
		c.schemaIDs[s.ID], c.schemaDef[s.ID] = owner, ext

		key := interfaceTypeKey{kind: s.AppliesToKind, typ: s.AppliesToInterfaceType}
		c.schemas[key] = append(c.schemas[key], schemaEntry{def: s, ownerURI: owner})
	}
	return nil
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
	if c.distinctDefinitions(owners) > 1 {
		return owners[0], KindMatchFallback, nil
	}
	return owners[0], "", nil
}

// distinctDefinitions counts the definitions behind owners. One definition
// reached under two references has two owners but is not an ambiguous kind.
func (c *Catalog) distinctDefinitions(owners []string) int {
	seen := make(map[any]struct{}, len(owners))
	for _, owner := range owners {
		if ext := c.ownerDef[owner]; ext != nil {
			seen[definitionKey(ext)] = struct{}{}
		} else {
			seen[owner] = struct{}{}
		}
	}
	return len(seen)
}

func (c *Catalog) IsValidInterfaceType(kind, interfaceType string) bool {
	_, ok := c.typeOwner[interfaceTypeKey{kind: kind, typ: interfaceType}]
	return ok
}

func (c *Catalog) schemasFor(kind, interfaceType string) []schemaEntry {
	return c.schemas[interfaceTypeKey{kind: kind, typ: interfaceType}]
}

// LoadAndMerge resolves rootID and its dependencies, then merges their
// declarations. Locations may be nil to use catalog lookup or the loader directly.
func LoadAndMerge(loader LoaderFunc, rootID string, locations map[string]string) (*Catalog, error) {
	resolver := NewResolver(loader)
	resolver.Locations = locations
	graph, err := resolver.Resolve(rootID)
	if err != nil {
		return nil, err
	}
	return Merge(graph)
}

func (c *Catalog) String() string {
	return fmt.Sprintf("Catalog{kinds=%d, interfaceTypes=%d, schemas=%d}", len(c.kindOwners), len(c.typeOwner), len(c.schemas))
}
