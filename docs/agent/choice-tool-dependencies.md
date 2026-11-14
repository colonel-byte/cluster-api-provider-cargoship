# Tool versions live in `go.mod`, except golangci-lint

`controller-gen`, `kustomize` and `setup-envtest` are `tool` directives in `go.mod`. The `Makefile` calls them through `go tool`, so there is no version to pin in two places, Dependabot owns the bumps, and a vendored checkout resolves them with no network access - which is the same property the rest of this repository has.

**`golangci-lint` is deliberately not one of them.** It runs from `.pre-commit-config.yaml`, which is also how `colonel-byte/cargoship` runs it.

## Why golangci-lint cannot be a tool dependency here

A tool directive puts the tool's dependencies in the module's own graph. golangci-lint's graph pulls `k8s.io/*` up to a newer patch release than Cluster API's, and the two are not compatible:

```
k8s.io/apimachinery@v0.37.2 pkg/util/managedfields/internal/typeconverter.go:51:
  cannot use typeSchema.Types (variable of type []sigs.k8s.io/structured-merge-diff/v7/schema.TypeDef)
  as []sigs.k8s.io/structured-merge-diff/v6/schema.TypeDef value in struct literal
```

`sigs.k8s.io/cluster-api@v1.14.4` is built against `structured-merge-diff/v6`. The newer `apimachinery` that golangci-lint drags in is built against `v7`. Both are in the graph, and `k8s.io/kube-openapi` resolves to a version that hands `v7` types to a `v6` signature - so **the whole module stops compiling**, including the two managers, and `go mod tidy` does not resolve it because tidy is what causes it.

The symptom is confusing enough to be worth writing down: the first thing that breaks is `golangci-lint` itself, reporting a `typecheck` error against `pkg/manager/manager.go` with ten levels of nested "could not import". That reads like a problem with the file. It is not.

## If you need to add a tool

Add it, then check `go build ./...` **and** `grep structured-merge-diff go.mod`. If the version moved, the tool is not safe to add here. The `k8s.io` modules are pinned to the versions `cluster-api` wants, not to latest, for this reason - a `go get -u` across them is not a routine upgrade.
