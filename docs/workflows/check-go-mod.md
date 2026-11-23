# Check for go mod changes

Source: [`check-go-mod.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/check-go-mod.yaml). Runs on every pull request and in the merge queue.

Three checks, each on a different way the dependency tree can disagree with itself:

- `go mod tidy` leaves `go.mod` and `go.sum` unchanged.
- `make vendor` leaves `vendor/` unchanged. `make vendor` rather than `go mod vendor` directly, because the target also writes the generated `osv-scanner.toml` files that `go mod vendor` deletes along with the rest of the tree.
- `make verify-vendor` passes, which catches a dependency bump that brings in a module carrying a lockfile no override covers.

This matters more here than in most repositories: `vendor/` is committed and every job builds with `-mod=vendor`, so a stale tree means CI tests different code than `go.mod` declares - silently, and in the direction of passing.

The `k8s.io/*` modules are held at the versions `sigs.k8s.io/cluster-api` was built against. Read [tool dependencies](../agent/choice-tool-dependencies.md) before raising a pin: a newer patch wants `structured-merge-diff/v7` while cluster-api pins v6, and the module stops compiling.

Dependabot re-vendors on every gomod update and trips the `vendor/` check every time; [fix-vendor-osv](fix-vendor-osv.md) pushes the regenerated overrides back onto its branch.
