package resolver

import (
	"errors"
	"testing"
)

func TestMerge_DistinctKindsAcrossExtensions(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  kinds: [{name: gadget}]
`),
	}
	g := mustResolve(t, docs, uriA)
	c, err := Merge(g)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	if !c.IsValidKind("widget") || !c.IsValidKind("gadget") {
		t.Fatalf("expected both kinds valid, got %v", c)
	}
	if !c.IsValidInterfaceType("widget", "http") {
		t.Fatal("expected widget/http interface type to be valid")
	}
	if c.IsValidKind("nope") {
		t.Fatal("expected unknown kind to be invalid")
	}
}

func TestMerge_ConflictingKindDeclarationFails(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  kinds: [{name: widget}]
`),
	}
	g := mustResolve(t, docs, uriA)
	_, err := Merge(g)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
	if conflictErr.Kind != "widget" {
		t.Fatalf("expected conflict on widget, got %q", conflictErr.Kind)
	}
}

func TestMerge_ConflictingInterfaceTypeDeclarationFails(t *testing.T) {
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
`),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
  interfaceTypes: [{name: http, targetKind: widget}]
`),
	}
	g := mustResolve(t, docs, uriA)
	_, err := Merge(g)
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
	if conflictErr.InterfaceType != "http" {
		t.Fatalf("expected conflict on interface type http, got %q", conflictErr.InterfaceType)
	}
}

func TestMerge_ConflictingSchemaBindingFails(t *testing.T) {
	schemaDoc := `
  schemas:
    - id: widget-http
      appliesToKind: widget
      appliesToInterfaceType: http
      schema: {type: object}
`
	docs := map[string][]byte{
		uriA: []byte(`
metadata: {id: ` + uriA + `}
spec:
  dependencies: [` + uriB + `]
  kinds: [{name: widget}]
  interfaceTypes: [{name: http, targetKind: widget}]
` + schemaDoc),
		uriB: []byte(`
metadata: {id: ` + uriB + `}
spec:
` + schemaDoc),
	}
	g := mustResolve(t, docs, uriA)
	_, err := Merge(g)
	var conflictErr *ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
}
