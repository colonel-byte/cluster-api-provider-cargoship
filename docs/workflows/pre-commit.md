# Validate pre-commit hooks

Source: [`pre-commit.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/pre-commit.yaml). Runs on every pull request and in the merge queue.

Runs the hooks from [`.pre-commit-config.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.pre-commit-config.yaml) over the whole tree with `prek`, so licensing, formatting and sorting can never drift from the local setup. Go is installed first, because the `language: system` hooks shell out to `make`.

Three hooks are skipped, each for a reason:

| Hook              | Why it is skipped                                                                              |
| ----------------- | ---------------------------------------------------------------------------------------------- |
| `controller-gen`  | Regenerates files rather than checking them; [verify-generated](verify-generated.md) covers it |
| `kustomize-build` | The same, through `make release-manifests`                                                     |
| `golangci-lint`   | Already covered by [scan-lint](scan-lint.md), at the same pinned version                       |

`markdown-table-padding` is deliberately **not** skipped: it is a check rather than a rewrite, and nothing else in the repository reads a Markdown table.
