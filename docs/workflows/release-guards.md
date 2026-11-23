# Release guards

Source: [`release-guards.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/release-guards.yaml). Runs on every pull request and in the merge queue.

Two greps, both for things that break a release silently and long after the pull request that caused them. Neither is covered by anything else here, and both take seconds.

**No local-path `replace` directives.** While a needed cargoship change is unreleased, `go.mod` can point at a local checkout. That builds perfectly for whoever has the checkout and for nobody else, and `vendor/` hides it completely - CI goes green because it is building the vendored snapshot. The release that follows cannot be built from source by anyone. Module-to-module replaces are left alone, since those are legitimate and resolvable.

**`metadata.yaml` agrees with the vendored cluster-api.** `metadata.yaml` tells clusterctl which Cluster API contract a release of this provider speaks, and nothing derives it from the code. A cluster-api bump that crosses a contract boundary leaves it asserting a contract the provider no longer implements, and clusterctl believes it.

The contract is read from `vendor/sigs.k8s.io/cluster-api/api/core/` rather than from `go list -m -f '{{.Dir}}'`, which returns nothing in vendor mode - the trap that made the first version of this check pass by comparing against an empty string.
