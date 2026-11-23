# Publish the Cluster API Operator artifact

Source: [`publish-oci-artifact.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/publish-oci-artifact.yaml). Called by [goreleaser](release.md) on a `v` tag, and runnable on demand from the Actions tab.

The third way the Cluster API Operator - and so Rancher Turtles - can install this provider: not from the release page and not from ConfigMaps, but from one OCI artifact holding `metadata.yaml` and both halves' component manifests. See [the Rancher Turtles guide](../guides/rancher-turtles.md).

`make oci-artifact` deliberately builds a layout rather than pushing one, because the registry an air gap uses is on the other side of it from the machine that builds it. This workflow is the exception that does push, to `ghcr.io/colonel-byte/turtle/cargoship`, which is what somebody mirrors *from*. It pushes with `oras cp --from-oci-layout` out of the layout directory rather than the tarball the target also writes.

The artifact is **signed by digest, never by tag**: a signature against a tag says nothing about which bytes were signed once the tag can move.

Running it on demand against an existing tag is how a release is republished without re-running a whole release.
