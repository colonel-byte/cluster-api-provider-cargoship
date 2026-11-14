# The Zarf package is built from `out/`, and pins its version three times

This repository ships a Zarf `v1beta1` package definition, [`zarf.yaml`](../../zarf.yaml), for management clusters with no route to `ghcr.io`. Two things about it look like oversights and are not.

## It deploys `out/*-components.yaml`, not the kustomizations

Zarf can run kustomize itself - a manifest takes `kustomize.files` as readily as `files` - so the package could point at `bootstrap/config/default` and skip `make release-manifests` entirely. It does not, because then the provider would be assembled two ways: once by `make release-manifests` for the operator who installs with `clusterctl`, and once by Zarf for the operator who installs in an air gap. Both would be correct on the day they were written and free to drift afterwards. Pointing at the release artifact means the air-gapped install is the *same* artifact, and `make zarf-package` builds it first so there is nothing to forget.

## The version and both image tags are literals, repeated

`###ZARF_PKG_TMPL_TAG###` does not reach an image reference in a `v1beta1` package. Zarf parses the reference during validation, which runs before `--set` is applied, and the create fails with `invalid reference format` - with `zarf dev lint` reporting the same thing, so this is cheap to re-confirm:

```sh
zarf dev lint . --set TAG=0.1.0
```

So the tag is written out three times, and release-please rewrites all three on a release: `zarf.yaml` is in `extra-files` in [`release-please-config.json`](../../release-please-config.json), and each of the three lines carries the `x-release-please-version` comment it looks for. Removing one of those comments is silent - the package still builds, pinning an image tag a release ago.

`make zarf-package` reads the version back out of `zarf.yaml` and renders the component manifests at it, rather than taking a `TAG` like the other release targets. A `TAG` there would produce a package whose manifests name one version and whose images carry another, which fails at deploy time against the in-cluster registry rather than at build time.

## What it deliberately leaves out

Core Cluster API, and the cargoship distro packages the control-plane controller pulls at converge time. Both are somebody else's artifact with their own cadence, and bundling either would make this package a mirror of the whole air gap rather than of this provider. [`docs/guides/install.md`](../guides/install.md) says so where an operator will read it.
