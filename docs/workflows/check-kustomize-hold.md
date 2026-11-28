# Check the kustomize hold

Source: [`check-kustomize-hold.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/check-kustomize-hold.yaml). Runs weekly on a schedule, and on demand.

`go.mod` holds `sigs.k8s.io/kustomize/*` below the latest patch release, because the newer releases require a `k8s.io/kube-openapi` that moved to `structured-merge-diff` v7 while the pinned apimachinery still compiles against v6. Taking the bump breaks every job with `cannot use typeSchema.Types ... v7/schema.TypeDef as v6/schema.TypeDef`.

Nothing in the tree records when that stops being true, and the hold is on tool-only dependencies nobody imports, so it would sit there unnoticed indefinitely. This job tries the bump every week - `go get ...@latest`, `go mod tidy`, `go mod vendor`, `go build ./...` - and opens or updates an issue the week it compiles.

It never commits anything. Picking versions is Renovate's job, and the lift wants a human to run the tests; the issue carries the `go.mod` diff and asks for `make verify` to be run by hand on the real change.

An unchanged `go.mod` means kustomize is already at the latest release, so there is no hold left to lift and nothing is reported. That is checked before the build, because a no-op upgrade builds fine and would otherwise open an issue every week.

This is the only workflow here that is allowed to reach the module proxy, which is the whole point of it - so it carries no `GOFLAGS: -mod=vendor`.
