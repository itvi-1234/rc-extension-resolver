package resolver

import (
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("extension not found")

type FetchError struct {
	URI string
	Err error
}

func (e *FetchError) Error() string {
	return fmt.Sprintf("fetch %s: %v", e.URI, e.Err)
}

func (e *FetchError) Unwrap() error { return e.Err }

type CycleError struct {
	Path []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("dependency cycle: %s", strings.Join(e.Path, " -> "))
}

// InterfaceType is empty when the conflict is on a kind itself.
type ConflictError struct {
	Kind          string
	InterfaceType string
	DeclaredBy    []string
}

func (e *ConflictError) Error() string {
	subject := e.Kind
	if e.InterfaceType != "" {
		subject = fmt.Sprintf("%s/%s", e.Kind, e.InterfaceType)
	}
	return fmt.Sprintf("conflict on %q: declared by %s", subject, strings.Join(e.DeclaredBy, ", "))
}
