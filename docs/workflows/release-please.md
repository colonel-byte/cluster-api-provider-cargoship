# Release Please

Source: [`release-please.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/release-please.yaml). Runs on every push to `main`, and on demand.

Maintains the release pull request: it reads the conventional commit titles since the last tag, works out the next version, updates `CHANGELOG.md` and every file listed as an `extra-file` in `release-please-config.json`, and opens or updates a pull request with the result. Merging that pull request is what pushes the tag, and the tag is what starts [goreleaser](release.md).

The whole thing runs on a **GitHub App token**, not `GITHUB_TOKEN`, and that is the load-bearing detail: a tag pushed with `GITHUB_TOKEN` does not trigger another workflow, so `release.yaml` would never fire and a release would be tagged with no artifacts behind it. The top-level permissions are read-only for the same reason - every write goes through the app token.

The `extra-files` list is how `metadata.yaml`, `zarf.yaml`, the install guide and the Rancher Turtles manifests come to name the version being released. A file that hard-codes a version and is not on that list goes stale at the next release and nothing says so.
