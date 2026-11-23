# CodeQL

Source: [`codeql.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/codeql.yaml). Runs on every pull request, on pushes to `main`, in the merge queue, and weekly on a schedule. Results go to the repository's Security tab.

Two languages in a matrix. `go` is built rather than scanned buildless, so the build step is what decides which packages get extracted; it is `go build -mod=vendor ./...`, with `GOPROXY=off` and `CGO_ENABLED=0`. `actions` is scanned with `build-mode: none`, which covers the workflows in this directory.

`./...` is correct here: this module has one Go module, no build-tagged dev tooling that cannot compile on its own, and no second module of upstream source, so there is nothing to carve out.

The weekly schedule is the part worth keeping. A scan only reports what was known when it ran, and most advisories are published after the commit they affect has landed.
