# Publish the Zarf package

Source: [`publish-zarf-package.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/publish-zarf-package.yaml). Called by [goreleaser](release.md) on a `v` tag, and runnable on demand from the Actions tab.

The air-gapped install path, published where the rest of this organisation's packages live: `ghcr.io/colonel-byte/zarf/<package>`, which `zarf package publish` assembles from that namespace and the package's own name and version. See [the install guide](../guides/install.md) and [the zarf package record](../agent/choice-zarf-package.md).

One job, a matrix over `amd64` and `arm64`, with `max-parallel: 1`. The serialisation is not caution: a Zarf reference is the package's name and version with no architecture in it, so both architectures publish to the same reference and two concurrent pushes to one tag is a race over which index wins.

It checks that `zarf.yaml` names the version being released before building, because `zarf package create` pulls the controller images the package carries by tag - a stale `zarf.yaml` would build a package full of the previous release.

There is no `ref:` on the checkout. A called workflow already runs at its caller's ref, which is the tag being released and the only commit whose `zarf.yaml` names this release's images.
