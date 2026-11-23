# A package is discovered by name through the registry's own records, never by catalogue

`spec.distro` names a cargoship package one of two ways: an `ociRef`, which says where it is, or a `registry` and a `name`, which asks the registry what it has. The second reads the records `cargoship publish --record` and `cargoship mirror` leave behind - `distro.ListMirrored`, new in cargoship 0.42.0. This records the parts of that which look arbitrary from the code.

## One tag listing, not a catalogue read

Every record a registry holds lives in one repository, `<registry>/cargoship`, tagged `_package.NAME.VERSION` or `_mirror.NAME.VERSION`. So discovery is a `tags/list` against a repository whose name is known, which needs no more permission than a pull - and the pull credentials `spec.distro.pullSecretRef` already names are therefore enough.

The alternative, finding packages by enumerating the registry's repositories, cannot work where this provider is aimed. `/v2/_catalog` is a wider permission than pushing wherever it exists at all: ghcr.io answers it `403 DENIED`, Docker Hub does not implement it, and the push credentials Zarf generates frequently cannot read it. Cargoship's [`choice-mirror-record-layout.md`](https://github.com/colonel-byte/cargoship/blob/main/docs/agent/choice-mirror-record-layout.md) measured that against the real registries and chose the layout accordingly; this is the consuming half of that decision.

## Only a package record is something to converge from

The two record kinds describe different things, and the difference matters more here than it does to `cargoship ls`:

| Kind      | Written by                   | What is in the registry                                                       |
| --------- | ---------------------------- | ----------------------------------------------------------------------------- |
| `package` | `cargoship publish --record` | One artifact at `<registry>/<name>:<version>`, which is what a converge pulls |
| `mirror`  | `cargoship mirror`           | The images and files the package declares, pushed under their own references  |

A registry filled by `cargoship mirror` alone holds everything a *node* needs and nothing the *controller* does, because there is no package artifact in it to pull. That is the one failure worth its own message, and `pickRecord` gives it one: the fix is a `cargoship publish --record` into the same registry, not a different version. An air gap wants both halves, and they are two commands.

## `status.distro.recordDigest` is not a digest to pin to

A record carries a `digest`, and the obvious move is to report it as `status.distro.digest` so an operator can paste it into an `ociRef`. It would be wrong. The record's digest is `layout.Digest()`, computed over the package's contents, while a published package's root is a multi-architecture index whose digest is a different number - and for a mirror record there is no artifact in the registry at all. So the two live in separate status fields, and `status.distro.digest` stays empty for a discovered package.

What the record's digest is good for is noticing change: it moves when a version's contents move, and not otherwise. That is why it is in the desired-state fingerprint rather than only in status - and why it is in it only when asked for, below. It does **not** close [`choice-distro-pinning.md`](choice-distro-pinning.md), which is about resolving a tag to the digest you could pull by; that still needs a cargoship-side resolve with no layer selector.

## Nothing is adopted without an operator asking

Two pieces of this would otherwise move a cluster that nobody touched, and both are deliberately refused:

- **`packageVersion` is required alongside `name`**, rather than defaulting to the newest version recorded. Taking the newest makes the registry the desired state and the custom resource a follower of it, so staging a package on a Friday afternoon would converge a fleet over the weekend.
- **A republished version is followed only with `controlplane.cluster.x-k8s.io/adopt-republished-package: "true"`** on the `CargoshipControlPlane`. Without it the new content digest is reported in `status.distro.recordDigest` and the fingerprint is unchanged, so an operator can see that the registry moved and decide when the fleet does. The annotation value is matched against `"true"` exactly; anything else is not consent.

The asymmetry is the point. Discovery is allowed to answer "what is in this registry" on every reconcile, because reading is free and a listing is a few kilobytes. Acting on a different answer than last time is an upgrade, and this provider has no rollout - a converge onto a package installing another Kubernetes version is refused rather than half-applied, so an automatic one would be a failed reconcile at best. See [`choice-no-rollout.md`](choice-no-rollout.md).

## The credentials are staged earlier for this path, and only this path

Resolution reads the registry, so the pull credentials and the CA have to be in place before the reference is resolved rather than afterwards, where the converge picks them up. `stageRegistryAccess` is called before resolution when `spec.distro.name` is set and in its original place otherwise, which means a discovering resource rewrites its Docker configuration file on a resync that would not otherwise have touched it. That cost is why it is gated on the path that needs it instead of being hoisted for everybody.
