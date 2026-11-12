# Installing the provider

## Register it with clusterctl

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

## The SSH key

The only secret the provider needs. The controller connects to every node with it, so it is cluster-admin-equivalent — treat it accordingly.

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

The control-plane controller pulls and extracts cargoship distro packages, so it needs writable storage. The shipped manifest mounts an `emptyDir` with a 20 GiB limit at `/var/lib/cargoship`.

One extracted package is as large as the package, so **raise the limit to suit the packages in use** — an `emptyDir` that fills takes the node's filesystem with it. Replace it with a `persistentVolumeClaim` where the registry is far away: the blob cache survives a restart, which is the expensive part to lose.

Note what the cache does and does not save. It saves the *fetch*. The package is extracted once per converge either way.

## Checking it is up

```sh
kubectl -n cargoship-controlplane-system logs deploy/cargoship-controlplane-controller-manager
kubectl -n cargoship-bootstrap-system logs deploy/cargoship-bootstrap-controller-manager
```

A healthy control-plane controller logs the cache and temp paths it resolved at startup. If it exited instead, those paths are the reason: it refuses to start rather than fall back to a default that would put the cache on the pod's writable layer.
