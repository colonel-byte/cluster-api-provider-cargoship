# Installing the provider with Rancher Turtles

[Rancher Turtles](https://turtles.docs.rancher.com) installs Cluster API providers through the Cluster API Operator rather than through `clusterctl`, so the [install guide](install.md)'s `clusterctl init` is not the install path on a Turtles management cluster. Two `CAPIProvider` objects are, and nothing else in that guide changes: the SSH key secret, the control-plane controller's storage, and the contract labels on the CRDs are all the same install.

## Declare both halves

The namespaces are not created for you, and the operator installs each provider into the namespace of the object that asked for it. Using the two this repository's manifests already name keeps the log commands in the install guide working:

```sh
kubectl create namespace cargoship-bootstrap-system
kubectl create namespace cargoship-controlplane-system
```

```yaml
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: CAPIProvider
metadata:
  name: cargoship-bootstrap
  namespace: cargoship-bootstrap-system
spec:
  name: cargoship
  type: bootstrap
  version: v0.1.0
  fetchConfig:
    url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases
---
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: CAPIProvider
metadata:
  name: cargoship-control-plane
  namespace: cargoship-controlplane-system
spec:
  name: cargoship
  type: controlPlane
  version: v0.1.0
  fetchConfig:
    url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases
```

Four things in there are easy to get wrong:

- **`spec.name` is the provider name, not `metadata.name`.** Turtles resolves the provider from `spec.name` and falls back to `metadata.name` only when it is unset, so both objects carry `cargoship` and differ by type - the same shape as the two `cargoship` entries in the `clusterctl.yaml` the install guide shows. Two objects in one namespace cannot both be called `cargoship`, which is why the names above are suffixed.
- **`spec.type` is `bootstrap` and `controlPlane`**, lower-camel. Not `BootstrapProvider`, which is the spelling the `ClusterctlConfig` below wants instead.
- **`spec.version` must be semver with a leading `v`**, and it names the release tag. A CEL rule on the CRD rejects `0.1.0`. With `fetchConfig.url` pointing at the releases page rather than at a file, it is also the only thing telling the operator which release to read, including the `metadata.yaml` beside the components that carries the contract version.
- **Leave `variables` and `configSecret` unset.** Both exist to substitute `clusterctl` variables into a provider's components, and this provider's components hold none.

## Or register it centrally

Where the provider should be known to the whole cluster rather than to one `CAPIProvider` - or where the components come from a mirror rather than from GitHub - put the URLs in Turtles' `ClusterctlConfig` and drop `fetchConfig` from the objects above:

```yaml
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: ClusterctlConfig
metadata:
  name: clusterctl-config
  namespace: cattle-turtles-system
spec:
  providers:
    - name: cargoship
      type: BootstrapProvider
      url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases/v0.1.0/bootstrap-components.yaml
    - name: cargoship
      type: ControlPlaneProvider
      url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases/v0.1.0/control-plane-components.yaml
```

The name and namespace are not free. Turtles merges this object into the `clusterctl-config` ConfigMap it ships, and it looks the object up at that ConfigMap's own name and namespace - `clusterctl-config`, in whatever namespace Turtles runs in, which defaults to `cattle-turtles-system` and is `rancher-turtles-system` on older installs. One created anywhere else is read by nothing, and nothing says so. Ask the cluster rather than guessing:

```sh
kubectl get configmap clusterctl-config --all-namespaces
```

That ConfigMap is also where the core Cluster API version Turtles installs is written down. This provider declares the `v1beta2` contract in its `metadata.yaml`, so the core version there has to be one that speaks it:

```sh
kubectl get configmap clusterctl-config -n cattle-turtles-system -o jsonpath='{.data.clusterctl\.yaml}'
```

## Check it came up

```sh
kubectl get capiproviders --all-namespaces
```

Both rows reach `Ready` in `Phase`, with `cargoship` as `ProviderName` and the release tag as `InstalledVersion`. A row stuck in `Provisioning` is the operator failing to fetch or apply the components, and its reason is in the Turtles controller's log rather than on the object.

## Importing the workload cluster into Rancher

Turtles imports a Cluster API cluster into Rancher when the `Cluster` - or the namespace holding it - carries the auto-import label:

```sh
kubectl label namespace my-clusters cluster-api.cattle.io/rancher-auto-import=true
```

Rancher then installs its cluster agent into the workload cluster, which needs a working CNI there and an API endpoint the management cluster can reach. A cargoship cluster that has converged has both; one still waiting on machines has neither, and the import simply happens later. Nothing in this provider is involved in the import either way.

## What this does not solve

`clusterctl` and Turtles are two ways to install the same component manifests, and installing with one leaves the other unaware. If the provider was installed with [the Zarf package](install.md#installing-without-a-route-to-ghcrio) instead, Turtles does not manage it and `kubectl get capiproviders` shows nothing - deploy the next package to upgrade, or delete it and declare the `CAPIProvider` objects to hand it over.
