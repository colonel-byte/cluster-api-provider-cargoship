# goreleaser

Source: [`release.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/release.yaml). Runs on every pushed tag.

The release itself, and two reusable workflows chained behind it.

`goreleaser` checks out with full history - goreleaser reads the tag history to work out the version and build the changelog - installs syft, cosign, Go, QEMU and buildx, logs in to ghcr.io, and runs `goreleaser release --clean`. That publishes the manager binaries, the component manifests clusterctl fetches, both controller images, the checksums and an SBOM, all signed with the cosign key.

Then three things that are easy to assume and expensive to get wrong:

- **clusterctl's three files are checked by name.** A release missing `bootstrap-components.yaml`, `control-plane-components.yaml` or `metadata.yaml` fails at `clusterctl init` time with a 404 that names a URL rather than a build step, and the fix is a whole new tag.
- **Provenance is attested** for the archives, the images and `ai-bom.json`, and the bundles are uploaded to the release so they can be verified with `gh attestation verify`.
- **`ai-bom.json` is rendered here**, stamped with this release's version, commit and build time. It is a per-release snapshot of what `.github/ai-bom.json` claims rather than a list of anything in the release, which is why goreleaser does not build it.

[publish-oci-artifact](publish-oci-artifact.md) and [publish-zarf-package](publish-zarf-package.md) then run in sequence, each on `needs`. Both carry the images this release pushes, so neither can run before goreleaser has pushed them - and watching for the release event instead does not work, because release-please publishes the release roughly ten minutes before there is an image to pull. Both are gated on a `v` prefix, since goreleaser takes every tag while release-please only ever pushes `vN.N.N`.

Permissions are read-only at the top so a job has to ask for what it writes, and each called workflow's `permissions:` block matches the called job's own: a caller's grant is the ceiling for what it calls, so these can be narrowed down the chain and never widened.
