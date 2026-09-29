# rc-extension-resolver

Pulls, caches, and validates Runtime Conditions extensions for downstream processing.

## What it does

A Runtime Conditions Profile lists the extensions it depends on and a set
of Conditions that use those extensions' kinds, interface types, and JSON
Schemas. This library does three things:

1. **Resolve** - fetch an extension and everything it depends on. Fails on
   cycles or broken references.
2. **Merge** - combine every resolved extension into one `Catalog`. Fails
   if two extensions define the same interface type or schema.
3. **Validate** - check a Condition against the catalog: is its `kind`
   known, is its `interface.type` known, does it pass the bound JSON
   Schemas.

Extension authors are expected to namespace their `kind` names so they
don't collide with anyone else's. The library doesn't enforce that, so a
Condition can still name an `extension` field to pick between two
extensions that do collide on `kind` - but that's a fallback for a
naming mistake, not something to design around.

It doesn't care where an extension document lives - `LoaderFunc` is just
`func(uri string) (io.ReadCloser, error)`, so HTTP, disk, or an in-memory
map all work the same way.

## Usage

```go
loader := resolver.NewHTTPLoader(10 * time.Second)

catalog, err := resolver.LoadAndMerge(loader, profile.Extensions[0])
if err != nil {
    // resolution or merge failed
}

result, err := catalog.ValidateCondition(condition)
if err != nil {
    // condition references something the catalog doesn't know about
}
if !result.Valid {
    // result.Errors has the schema validation failures
}
```

## Install

```sh
go get github.com/runtimeconditions/rc-extension-resolver
```
