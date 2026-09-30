package resolver

import (
	"errors"
	"testing"
)

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

// ambiguousKindCatalog mirrors the spec's cache/redis vs cache/memcached
// example: two independently authored extensions both define kind cache,
// distinguished by interface type.
func ambiguousKindCatalog(t *testing.T) *Catalog {
	t.Helper()
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  kinds: [{name: cache}]
  interfaceTypes: [{name: redis_protocol, targetKind: cache}]
  schemas:
    - id: cache-redis
      appliesToKind: cache
      appliesToInterfaceType: redis_protocol
      schema: {type: object}
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  kinds: [{name: cache}]
  interfaceTypes: [{name: memcached_protocol, targetKind: cache}]
`),
	}
	// uriA and uriB are independent roots; mimic resolveAll's fan-out by
	// resolving each and concatenating, uriA first.
	ra := mustResolve(t, docs, uriA)
	rb := mustResolve(t, docs, uriB)
	g := &ResolvedGraph{ByID: map[string]*ExtensionDefinition{}}
	for _, ext := range append(ra.Extensions, rb.Extensions...) {
		if _, ok := g.ByID[ext.Metadata.ID]; ok {
			continue
		}
		g.ByID[ext.Metadata.ID] = ext
		g.Extensions = append(g.Extensions, ext)
	}
	c, err := Merge(g)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	return c
}

func TestValidateCondition_AmbiguousKindExplicitExtensionMatches(t *testing.T) {
	c := ambiguousKindCatalog(t)
	result, err := c.ValidateCondition(map[string]any{
		"kind":      "cache",
		"extension": uriA,
		"interface": map[string]any{"type": "redis_protocol"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.KindMatch != KindMatchExplicit {
		t.Fatalf("expected explicit match, got %q", result.KindMatch)
	}
	if result.ResolvedExtension != uriA {
		t.Fatalf("expected resolved extension %q, got %q", uriA, result.ResolvedExtension)
	}
}

func TestValidateCondition_AmbiguousKindNoExtensionFallsBackToFirst(t *testing.T) {
	c := ambiguousKindCatalog(t)
	result, err := c.ValidateCondition(map[string]any{
		"kind":      "cache",
		"interface": map[string]any{"type": "redis_protocol"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.KindMatch != KindMatchFallback {
		t.Fatalf("expected fallback match, got %q", result.KindMatch)
	}
	if result.ResolvedExtension != uriA {
		t.Fatalf("expected fallback to first-resolved extension %q, got %q", uriA, result.ResolvedExtension)
	}
}

func TestValidateCondition_AmbiguousKindWrongExtensionIsError(t *testing.T) {
	c := ambiguousKindCatalog(t)
	_, err := c.ValidateCondition(map[string]any{
		"kind":      "cache",
		"extension": "mem://not-a-resolved-extension.yaml",
		"interface": map[string]any{"type": "redis_protocol"},
	})
	if err == nil {
		t.Fatal("expected error for extension that doesn't define this kind")
	}
	var mismatchErr *ExtensionMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected *ExtensionMismatchError, got %T: %v", err, err)
	}
}

func TestValidateCondition_UnambiguousKindHasNoMatchType(t *testing.T) {
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
	if result.KindMatch != "" {
		t.Fatalf("expected no match type for an unambiguous kind, got %q", result.KindMatch)
	}
}

func TestValidateCondition_UnresolvedSchemaRefIsError(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
  schemas:
    - id: widget-http-broken
      appliesToKind: widget
      appliesToInterfaceType: http
      schema:
        $schema: https://json-schema.org/draft/2020-12/schema
        $ref: '#/$defs/nope'
`),
	}
	g := mustResolve(t, docs, uriA)
	c, err := Merge(g)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	_, err = c.ValidateCondition(map[string]any{
		"kind":      "widget",
		"interface": map[string]any{"type": "http"},
	})
	if err == nil {
		t.Fatal("expected error: schema has a $ref that never resolves")
	}
}
