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

## Air-gapped, with nothing to fetch

`fetchConfig` takes a `selector` instead of a `url`, and then the operator reads the components and the metadata out of ConfigMaps in the cluster - so an air-gapped management cluster needs no route to GitHub at all, and no mirror to stand in for it. One ConfigMap per half, carrying the same two files a release does under the keys `components` and `metadata`.

On the connected side:

```sh
gh release download v0.1.0 --repo colonel-byte/cluster-api-provider-cargoship \
  --pattern 'bootstrap-components.yaml' --pattern 'control-plane-components.yaml' --pattern 'metadata.yaml'
```

Inside:

```sh
kubectl create configmap v0.1.0 --namespace cargoship-bootstrap-system \
  --from-file=components=bootstrap-components.yaml \
  --from-file=metadata=metadata.yaml
kubectl label configmap v0.1.0 --namespace cargoship-bootstrap-system provider-components=cargoship

kubectl create configmap v0.1.0 --namespace cargoship-controlplane-system \
  --from-file=components=control-plane-components.yaml \
  --from-file=metadata=metadata.yaml
kubectl label configmap v0.1.0 --namespace cargoship-controlplane-system provider-components=cargoship
```

Then both objects lose `fetchConfig.url` and gain the selector, with everything else unchanged:

```yaml
  fetchConfig:
    selector:
      matchLabels:
        provider-components: cargoship
```

- **The ConfigMap's name is the version the operator offers**, unless it carries a `provider.cluster.x-k8s.io/version` label saying otherwise - and it has to match the `spec.version` on the `CAPIProvider`. Two ConfigMaps both called `v0.1.0` is fine here only because each sits in its own namespace; in one namespace, give them different names and a version label each.
- **The label is yours.** `provider-components` is only the convention the operator's own examples use; nothing reads the key, the selector just has to match.
- **The components still name `ghcr.io` images.** Mirror the two controller images and override the registry through the `CAPIProvider`'s `configSecret`, whose `images` key the operator reads as a clusterctl image override - `all: {repository: <registry>/<org>}`. This is the one part of the air-gapped install that a selector does not cover. The distro package the controller pulls later is separate again: see [pulling it from a Zarf registry](registry.md).
- **A ConfigMap holds 1 MiB.** These manifests are tens of kilobytes, so there is room to spare; if that ever stops being true, `gzip` the components and annotate the ConfigMap `provider.cluster.x-k8s.io/compressed: "true"`, without which the operator reads the archive as YAML.

## Air-gapped, from a registry

`fetchConfig.oci` is the third option, and the one to reach for where the air gap already has a registry: the operator pulls `metadata.yaml` and the components out of a single OCI artifact, which is one object to mirror rather than four ConfigMaps to keep in step. `make oci-artifact` builds that artifact, holding both halves of this provider.

On the connected side:

```sh
make oci-artifact TAG=v0.1.0
```

That writes an OCI image layout to `out/oci`, tagged `v0.1.0`, and tars it as `out/cargoship-capi-providers-v0.1.0.oci.tar` - which is the file to carry in. Nothing is pushed: the registry this is for is on the other side of the air gap from the machine that builds it.

Inside, fill the mirror from the tar and point both objects at it:

```sh
oras cp --from-oci-layout ./cargoship-capi-providers-v0.1.0.oci.tar:v0.1.0 \
  registry.internal/cargoship/capi-providers:v0.1.0
```

```yaml
  fetchConfig:
    oci: registry.internal/cargoship/capi-providers:v0.1.0
```

Four things worth knowing before you build your own artifact by hand:

- **The file names in the artifact are the typed ones** - `bootstrap-components.yaml`, `control-plane-components.yaml` and `metadata.yaml` - carried in each layer's `org.opencontainers.image.title` annotation, which is the only thing the operator reads a layer by. The media types do not matter; the operator's own publisher pushes `application/vnd.test.file`.
- **Do not use the versioned names here.** The operator looks for `<type>-<name>-<version>-components.yaml` first, and that middle segment is the name of the *custom resource*, not the provider: with the objects above it would be hunting for `bootstrap-cargoship-bootstrap-v0.1.0-components.yaml`. The typed names depend on nothing but the provider type, so one artifact works whatever the objects are called.
- **A reference with no tag takes `spec.version` as the tag**, so `oci: registry.internal/cargoship/capi-providers` and a `spec.version` of `v0.1.0` resolve to the same artifact as the reference above. A registry served over plain HTTP needs an `http://` prefix on the reference, which is how the operator decides to skip TLS.
- **Credentials come from provider variables, not from a pull secret.** The operator reads `OCI_USERNAME` and `OCI_PASSWORD`, or `OCI_ACCESS_TOKEN` or `OCI_REFRESH_TOKEN`, out of the `CAPIProvider`'s `configSecret` - the same secret the registry override below goes in. A mirror that allows anonymous reads needs none of them.

The components still name `ghcr.io` images, exactly as they do when they come from ConfigMaps, so the note above about mirroring the two controller images applies here unchanged.


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

`clusterctl` and Turtles are two ways to install the same component manifests, and installing with one leaves the other unaware. If the provider was installed with [the Zarf package](install.md#installing-without-a-route-to-ghcrio) instead, Turtles does not manage it and `kubectl get capiproviders` shows nothing - deploy the next package to upgrade, or delete it and declare the `CAPIProvider` objects to hand it over. On a Turtles cluster the selector above is the better air-gapped route of the two, because what it installs is still a provider Turtles manages.
