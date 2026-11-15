# Installing the provider

## Register it with clusterctl

On a Rancher Turtles management cluster, skip to [installing with Rancher Turtles](rancher-turtles.md) instead - Turtles installs providers through the Cluster API Operator, and the rest of this guide still applies.

Until the provider is in `clusterctl`'s built-in list, point it at the releases. Add this to `~/.config/cluster-api/clusterctl.yaml`:

```yaml
providers:
  - name: cargoship
    url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases/latest/bootstrap-components.yaml
    type: BootstrapProvider
  - name: cargoship
    url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases/latest/control-plane-components.yaml
    type: ControlPlaneProvider
```

Both providers come from one repository and one tag, so they are always version-matched.

## Install

```sh
clusterctl init \
  --infrastructure vsphere \
  --bootstrap cargoship \
  --control-plane cargoship
```

This installs two controller deployments, into `cargoship-bootstrap-system` and `cargoship-controlplane-system`.

## Installing without a route to ghcr.io

[`zarf.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/zarf.yaml) in the repository root is a [Zarf](https://docs.zarf.dev) `v1beta1` package definition carrying both controller images and the same two component manifests `clusterctl` would download.

Every release publishes it, for `amd64` and `arm64`, at **`ghcr.io/colonel-byte/zarf/cluster-api-provider-cargoship`** - tagged with the package's version, which is the release's tag without the leading `v`. Pull it where a registry is reachable:

```sh
zarf package pull oci://ghcr.io/colonel-byte/zarf/cluster-api-provider-cargoship:0.2.0
```

Or build it from the definition, which is what to do for a version that was never released or a definition you have edited:

```sh
make zarf-package     # writes out/zarf-package-cluster-api-provider-cargoship-<arch>-<version>.tar.zst
```

A build needs the release's controller images to be pullable, because the package carries them; the version it builds comes from `zarf.yaml` rather than from a `TAG`, for the reason [the decision record](../agent/choice-zarf-package.md) gives.

Carry the tarball in, and deploy it to a cluster that has had `zarf init` run on it - the in-cluster registry it installs is what the controller images are rewritten to point at:

```sh
zarf package deploy zarf-package-cluster-api-provider-cargoship-amd64-0.1.0.tar.zst
```

Three things it does not do, each of which will otherwise be found out later:

- **It does not install core Cluster API.** `clusterctl init` is still what puts the core controllers and an infrastructure provider in place, and in an air gap that means its own images and manifests mirrored by some other means.
- **It does not carry the cargoship distro packages.** The control-plane controller pulls the package named in `spec.distro.ociRef` over OCI at converge time, from inside the management cluster. Mirror it to a registry the cluster can reach and point the reference there - [pulling it from a Zarf registry](registry.md) covers the credentials and the certificate that usually come with one.
- **It does not stop a `ClusterClass` working.** The package deploys the same component manifests `clusterctl` would, so all three CRDs come with it - including `CargoshipControlPlaneTemplate`, the kind a class names as its control plane. A class, its templates and a `Cluster` with a topology are ordinary objects an operator applies afterwards; see [Composing a ClusterClass](clusterclass.md), and remember that `CLUSTER_TOPOLOGY` is a flag on core Cluster API, which this package does not install.
- **`clusterctl` does not know the provider is installed.** The component manifests carry the Cluster API contract labels, which is what the core controllers read, but not the `clusterctl.cluster.x-k8s.io` bookkeeping `clusterctl init` adds - so `clusterctl upgrade` will not see this provider. Upgrade it by deploying the next package.

## Installing Cluster API itself on a Zarf-initialized cluster

Worth knowing before `clusterctl init` on the same cluster Zarf runs on, because the failure names nothing to do with either tool. Zarf's mutating webhook rewrites every pod's image to its in-cluster registry, so `clusterctl init` installs cert-manager and the core controllers pointing at images that were never mirrored there:

```
Error: context deadline exceeded
# and, on the pods:
Failed to pull image "127.0.0.1:5000/jetstack/cert-manager-controller:v1.21.3-zarf-1554577349"
```

Label each namespace Zarf should leave alone before installing into it:

```sh
for ns in cert-manager capi-system capd-system; do
  kubectl create namespace "${ns}" --dry-run=client -o yaml | kubectl apply -f -
  kubectl label namespace "${ns}" zarf.dev/agent=ignore
done
```

Pods already admitted keep the rewritten reference, so delete them after labelling - the webhook only acts at admission. The alternative, for a cluster with no route out at all, is to mirror core Cluster API's images into the Zarf registry and leave the rewriting in place. Nothing about this provider is involved either way: the namespaces it installs into hold images the Zarf package already pushed.

## The SSH key

The only secret the provider needs. The controller connects to every node with it, so it is cluster-admin-equivalent - treat it accordingly.

```sh
ssh-keygen -t ed25519 -N '' -f cluster-key
kubectl create secret generic fleet-ssh \
  --namespace default \
  --type=kubernetes.io/ssh-auth \
  --from-file=ssh-privatekey=cluster-key
rm cluster-key
```

The secret must be in the same namespace as the `CargoshipControlPlane`. The key must not have a passphrase: there is no terminal in a controller to supply one, and an encrypted key is refused rather than prompted for.

The public half is **derived** from this secret by the bootstrap provider and authorized on every machine, so there is no second key to keep in step. Delete `cluster-key.pub` too; it is not used.

## Storage for the control-plane controller

The control-plane controller pulls and extracts cargoship distro packages, so it needs writable storage. Where that registry wants credentials or serves its own certificate, see [pulling the distro package from a Zarf registry](registry.md). The shipped manifest mounts an `emptyDir` with a 20 GiB limit at `/var/lib/cargoship`.

One extracted package is as large as the package, so **raise the limit to suit the packages in use** - an `emptyDir` that fills takes the node's filesystem with it. Replace it with a `persistentVolumeClaim` where the registry is far away: the blob cache survives a restart, which is the expensive part to lose.

Note what the cache does and does not save. It saves the *fetch*. The package is extracted once per converge either way.

## Checking it is up

```sh
kubectl -n cargoship-controlplane-system logs deploy/cargoship-controlplane-controller-manager
kubectl -n cargoship-bootstrap-system logs deploy/cargoship-bootstrap-controller-manager
```

A healthy control-plane controller logs the cache and temp paths it resolved at startup. If it exited instead, those paths are the reason: it refuses to start rather than fall back to a default that would put the cache on the pod's writable layer.
