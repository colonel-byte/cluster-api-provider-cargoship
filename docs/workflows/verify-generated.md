# Verify generated files

Source: [`verify-generated.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/verify-generated.yaml). Runs on every pull request and in the merge queue, with in-progress runs cancelled per ref.

Runs `make verify`, which regenerates everything and fails if a checked-in file would change, then `make release-manifests` to prove the kustomizations still build.

It exists because the CRDs, the RBAC roles, the deepcopy functions, the JSON Schemas and the book's generated pages are all committed, and the component manifests built from them are the actual release artifact. A type change that did not regenerate its CRD produces a release that installs the previous schema, and nothing else here would notice.

`make verify` compares only the paths the generators write, not the whole tree: a bare `git diff` would fail on any uncommitted edit and report it as "generated files are out of date", which sends the reader to the wrong place.
