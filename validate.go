package resolver

import (
	"encoding/json"
	"fmt"

	"github.com/kaptinlin/jsonschema"
)

type compiledSchema struct {
	schema *jsonschema.Schema
}

type ValidationResult struct {
	Valid  bool
	Errors []string
}

func (c *Catalog) ValidateCondition(condition map[string]any) (*ValidationResult, error) {
	kind, _ := condition["kind"].(string)
	if kind == "" {
		return nil, fmt.Errorf("condition has no kind")
	}
	iface, _ := condition["interface"].(map[string]any)
	interfaceType, _ := iface["type"].(string)
	if interfaceType == "" {
		return nil, fmt.Errorf("condition has no interface.type")
	}

	if !c.IsValidKind(kind) {
		return nil, fmt.Errorf("unknown kind %q", kind)
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
		return &ValidationResult{Valid: false, Errors: messages}, nil
	}
	return &ValidationResult{Valid: true}, nil
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

	c.compiled[entry.def.ID] = &compiledSchema{schema: schema}
	return schema, nil
}
