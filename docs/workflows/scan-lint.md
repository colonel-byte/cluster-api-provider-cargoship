# Validate Lint

Source: [`scan-lint.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/scan-lint.yaml). Runs on every pull request and in the merge queue.

`lint` runs golangci-lint at **the version the pre-commit hooks pin**, read out of `.pre-commit-config.yaml` by the step above it rather than written here as well. The action installs the latest release when it is given no version, which once meant CI ran 2.14.0 against a tree whose hooks pinned 2.13.2 - a lint failure that reproduced nowhere locally. Renovate's `pre-commit` manager bumps that `rev`, and its `github-actions` manager updates `uses:` references rather than inputs inside a `with:` block, so a second pin here would be one nothing keeps in step with the first.

`containers` runs hadolint, one matrix entry per Dockerfile. The paths are listed explicitly rather than globbed: hadolint's action takes a single path, and its recursive mode globs from the repository root, which drags in the Dockerfiles under `vendor/`.

golangci-lint is deliberately not a `go.mod` tool dependency - see [tool dependencies](../agent/choice-tool-dependencies.md).
