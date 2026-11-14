# Make for the targets the ecosystem names, mage for the generators behind them

Cargoship drives its builds, tests and generators through mage, and `magefiles/AGENTS.md` records why. This repository has both: a `Makefile` holding the target names Cluster API's tooling depends on, and `magefiles/` holding the generators those targets call.

## Why the Makefile owns the interface

The Cluster API ecosystem is Makefile-shaped, and not superficially:

- The provider implementers' documentation, the contributing guide and every reference provider assume `make generate`, `make manifests` and `make release-manifests`. A contributor who has worked on another provider knows those three targets and nothing else about this repository.
- `clusterctl`'s own release conventions are written in terms of a `RELEASE_DIR` a `make release-manifests` target fills. `.goreleaser.yaml` calls that target in its `before.hooks`, so the release pipeline depends on the name.
- The tooling is kubebuilder's - `controller-gen`, `kustomize`, `setup-envtest` - and all of it is documented as Makefile variables.

Those names are an interface, not an implementation detail. Nothing about what is behind them is visible to a contributor who types `make generate`.

## Why mage owns the generators

This repository started with one generator, `controller-gen`, which is a single command with flags and belongs in a Makefile recipe. The book is the second one, and it is not that: it reads the docs tree, works out each page's sidebar label, and writes `docs/SUMMARY.md` from what it finds, because a page that is added and not listed there is simply missing from the book and `mdbook build` does not say so.

That was a `cp` and two `sed` expressions, and it could not have grown into the summary generator without becoming unreadable and untestable. It is now `magefiles/pkg/gen/docs`, an ordinary Go package with tests, called by `make docs`, which `make generate` depends on. Shell in a Makefile recipe has no unit tests and no way to get them.

mage is pinned in `go.mod` as a tool dependency, the same way `controller-gen` and `kustomize` are, so `make docs` needs nothing installed and Dependabot owns the version - see [choice-tool-dependencies](choice-tool-dependencies.md).

## The rule for anything added later

**A generator that is one command with flags stays a Makefile recipe. A generator that has to read the repository and decide something goes in `magefiles/`, as a package with tests, and gets a Makefile target that calls it.**

The second half of that matters as much as the first: `mage` is never the documented way to run anything here. Everything is reachable through `make`, because that is what the ecosystem's documentation tells a contributor to type, and a second entry point that only some targets have is worse than either tool on its own.

## What is kept from cargoship's pattern

Everything about releasing and dependencies, which is the part that is genuinely house style rather than ecosystem convention: release-please with the same changelog sections and tag format, goreleaser with cosign signing against the same key, Dependabot with the same grouping and daily cadence, the same pre-commit hooks, a vendored `vendor/` tree, and `.gitattributes` grouped by the generator that writes each path. The generator layout is now shared too - `magefiles/pkg/gen/...`, with the magefile holding no logic of its own.
