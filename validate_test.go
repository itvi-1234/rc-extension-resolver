package resolver

import "testing"

func widgetCatalog(t *testing.T) *Catalog {
	t.Helper()
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
  schemas:
    - id: widget-http
      appliesToKind: widget
      appliesToInterfaceType: http
      schema:
        $schema: https://json-schema.org/draft/2020-12/schema
        type: object
        required: [kind, interface]
        properties:
          kind:
            const: widget
          interface:
            type: object
            required: [type, uri]
            properties:
              type:
                const: http
              uri:
                type: string
                minLength: 1
`),
	}
	g := mustResolve(t, docs, uriA)
	c, err := Merge(g)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	return c
}

func TestValidateCondition_ValidInstance(t *testing.T) {
	c := widgetCatalog(t)
	result, err := c.ValidateCondition(map[string]any{
		"kind": "widget",
		"interface": map[string]any{
			"type": "http",
			"uri":  "https://example.com",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateCondition_InvalidInstanceReportsErrors(t *testing.T) {
	c := widgetCatalog(t)
	result, err := c.ValidateCondition(map[string]any{
		"kind": "widget",
		"interface": map[string]any{
			"type": "http",
			// missing required "uri"
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid instance to fail validation")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one structured error")
	}
}

func TestValidateCondition_UnknownKindIsError(t *testing.T) {
	c := widgetCatalog(t)
	_, err := c.ValidateCondition(map[string]any{
		"kind":      "nope",
		"interface": map[string]any{"type": "http"},
	})
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestValidateCondition_UnknownInterfaceTypeIsError(t *testing.T) {
	c := widgetCatalog(t)
	_, err := c.ValidateCondition(map[string]any{
		"kind":      "widget",
		"interface": map[string]any{"type": "nope"},
	})
	if err == nil {
		t.Fatal("expected error for unknown interface type")
	}
}
