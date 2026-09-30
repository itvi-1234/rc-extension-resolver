package resolver

import (
	"encoding/json"
	"fmt"

	"github.com/kaptinlin/jsonschema"
)

type compiledSchema struct {
	schema *jsonschema.Schema
}

// KindMatch reports how an ambiguous Condition kind was resolved. It's the
// zero value when the kind had only one owner, since there was nothing to
// disambiguate.
type KindMatch string

const (
	KindMatchExplicit KindMatch = "explicit"
	KindMatchFallback KindMatch = "fallback"
)

type ValidationResult struct {
	Valid  bool
	Errors []string

	// ResolvedExtension is the extension whose kind definition this
	// Condition resolved against.
	ResolvedExtension string
	KindMatch         KindMatch
}

func (c *Catalog) ValidateCondition(condition map[string]any) (*ValidationResult, error) {
	kind, _ := condition["kind"].(string)
	if kind == "" {
		return nil, fmt.Errorf("condition has no kind")
	}
	extension, _ := condition["extension"].(string)
	iface, _ := condition["interface"].(map[string]any)
	interfaceType, _ := iface["type"].(string)
	if interfaceType == "" {
		return nil, fmt.Errorf("condition has no interface.type")
	}

	owner, match, err := c.resolveKind(kind, extension)
	if err != nil {
		return nil, err
	}
	if !c.IsValidInterfaceType(kind, interfaceType) {
		return nil, fmt.Errorf("unknown interface type %q for kind %q", interfaceType, kind)
	}

	entries := c.schemasFor(kind, interfaceType)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no schema bound to kind %q interface type %q", kind, interfaceType)
	}

	var messages []string
	for _, entry := range entries {
		schema, err := c.compile(entry)
		if err != nil {
			return nil, err
		}
		result := schema.Validate(condition)
		if result.IsValid() {
			continue
		}
		for path, msg := range result.DetailedErrors() {
			messages = append(messages, fmt.Sprintf("[%s] %s: %s", entry.def.ID, path, msg))
		}
	}
	if len(messages) > 0 {
		return &ValidationResult{Valid: false, Errors: messages, ResolvedExtension: owner, KindMatch: match}, nil
	}
	return &ValidationResult{Valid: true, ResolvedExtension: owner, KindMatch: match}, nil
}

func (c *Catalog) compile(entry schemaEntry) (*jsonschema.Schema, error) {
	c.compileMu.Lock()
	defer c.compileMu.Unlock()

	if cached, ok := c.compiled[entry.def.ID]; ok {
		return cached.schema, nil
	}

	data, err := json.Marshal(entry.def.Schema)
	if err != nil {
		return nil, fmt.Errorf("marshaling schema %q: %w", entry.def.ID, err)
	}

	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(data, entry.def.ID)
	if err != nil {
		return nil, fmt.Errorf("compiling schema %q (from %s): %w", entry.def.ID, entry.ownerURI, err)
	}
	// jsonschema doesn't error on a $ref that never resolves - it just
	// treats the assertion as satisfied. Check for that ourselves so a
	// broken schema fails compilation instead of silently validating
	// everything.
	if unresolved := schema.UnresolvedReferenceURIs(); len(unresolved) > 0 {
		return nil, fmt.Errorf("compiling schema %q (from %s): unresolved references: %v", entry.def.ID, entry.ownerURI, unresolved)
	}

	c.compiled[entry.def.ID] = &compiledSchema{schema: schema}
	return schema, nil
}
