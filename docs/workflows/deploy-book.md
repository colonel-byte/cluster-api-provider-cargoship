# GitHub Pages

Source: [`deploy-book.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/deploy-book.yaml). Runs on every push to `main`, with in-progress runs cancelled per ref.

Builds the book with mdBook and pushes the result to the `gh-pages` branch, which is what serves <https://colonel-byte.github.io/cluster-api-provider-cargoship/>. That push is the only write it makes, and nothing here touches a pull request.

The checkout takes full history so that mdBook's git-dependent output - the per-page edit link, and the last-modified data - resolves against real history rather than a shallow clone.

It builds whatever `docs/` holds at that commit and regenerates nothing. `docs/index.md`, `docs/security.md` and `docs/SUMMARY.md` are committed generator output, so a page that was not regenerated before merging is published stale rather than rebuilt here - which is what [verify-generated](verify-generated.md) and the `controller-gen` pre-commit hook are for.

**What it does not cover:** a dead relative link. `mdbook build` does not fail on one, which is why the generator that writes `docs/security.md` checks its links itself.
