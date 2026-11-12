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

`LoadAndMerge(loader, rootID, locations)` accepts an optional retrieval map.
Pass `nil` to use automatic official-catalog lookup or load a reference directly.

Automatic catalog lookup recognizes `<url>:<version>` only when the URL uses
HTTPS, its hostname is exactly `runtimeconditions.io`, and both the URL and
version are present. For example:

```text
https://runtimeconditions.io/extensions/rc/common-integrations:v1alpha1
→ https://runtimeconditions.io/extensions/rc/common-integrations/v1alpha1/runtimeconditions.extension.yaml
```

Other references reach the loader unchanged. A custom loader can resolve opaque
identifiers, local files, or other reference forms. The supplied HTTP loader
fetches URLs. References retain their exact spelling in graph keys and Condition
extension selectors. A version of `.` or `..` is never expanded.

To resolve several roots into one graph, such as every item of a profile's
`extensions`, call `Resolver.ResolveAll(roots...)`. The graph is keyed by each
reference exactly as written, so a Condition's `extension` selector matches the
spelling the profile used.

Each resolved definition records the `Location` it was retrieved from. The same
file can be reached under more than one reference, for example
`https://runtimeconditions.io/extensions/rc/common-integrations:v1alpha1` from a
profile and the full file URL from another extension's `dependencies`. `Merge`
treats references with the same `Location` as one definition: they do not
conflict, a schema is bound once, and the kind is not reported as ambiguous.
Definitions from different locations that declare the same interface type or
schema still conflict.

Definition metadata stays separate: `metadata.id` is a non-empty string and
`metadata.version` is optional. For an official catalog reference, a declared
version must match the requested version; an omitted version is valid. Resolution
preserves both metadata fields.

```go
loader := resolver.NewHTTPLoader(10 * time.Second)

catalog, err := resolver.LoadAndMerge(loader, profile.Extensions[0], nil)
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

To override retrieval, supply a map keyed by exact reference strings. Map values
are locations passed to the loader. Mappings take precedence over automatic
lookup for roots and dependencies; unmapped references use normal resolution.
An explicitly empty mapped location is an error.

```go
locations := map[string]string{
    "example.extension": "https://example.test/definitions/extension.yaml",
}
catalog, err := resolver.LoadAndMerge(loader, "example.extension", locations)
```

The optional live catalog tests make real HTTPS requests:

```sh
go test -tags=live -run '^TestResolve_LiveOfficialCatalog$' ./...
```

## Install

```sh
go get github.com/runtimeconditions/rc-extension-resolver
```
