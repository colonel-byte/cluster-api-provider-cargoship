# This repository uses a Makefile, and `colonel-byte/cargoship` uses mage

Cargoship drives its builds, tests and generators through mage, and `magefiles/AGENTS.md` records why. This repository uses a plain `Makefile` instead.

## Why the difference

The Cluster API ecosystem is Makefile-shaped, and not superficially:

- The provider implementers' documentation, the contributing guide and every reference provider assume `make generate`, `make manifests` and `make release-manifests`. A contributor who has worked on another provider knows those three targets and nothing else about this repository.
- `clusterctl`'s own release conventions are written in terms of a `RELEASE_DIR` a `make release-manifests` target fills. `.goreleaser.yaml` calls that target in its `before.hooks`, so the release pipeline depends on the name.
- The tooling is kubebuilder's — `controller-gen`, `kustomize`, `setup-envtest` — and all of it is documented as Makefile variables.

The reason cargoship has mage targets in the first place does not apply here. Those exist to drive its own generators: the docs book, the JSON schemas, the example packages, the pinned upstream source. This repository has one generator, `controller-gen`, and it is a single command with flags.

## What is kept from cargoship's pattern

Everything about releasing and dependencies, which is the part that is genuinely house style rather than ecosystem convention: release-please with the same changelog sections and tag format, goreleaser with cosign signing against the same key, Dependabot with the same grouping and daily cadence, the same pre-commit hooks, a vendored `vendor/` tree, and `.gitattributes` grouped by the generator that writes each path.

If a generator ever lands here that mage would suit better, add mage alongside the Makefile rather than replacing it. The three target names above are an interface to the ecosystem, not an implementation detail.
