# Driving the provider by hand

[`e2e-tests.md`](e2e-tests.md) covers what the suites cover and why the Docker infrastructure provider cannot converge a fleet. This is the other half: how to put the provider on a real cluster, make a reconcile reach the step you want to look at, and check the parts no suite reaches - a registry read, a `ClusterClass` stamping a control plane, a `clusterctl` template rendering.

None of it needs hosts that answer SSH. All of it stops short of a converge, which is the point: the expensive thing to verify is the part before cargoship takes over.

## A cluster to work on

A `zarf init`-ed k3d cluster is the useful shape, because it is also the environment the provider is aimed at - an in-cluster registry with a private CA, credentials in a state secret, and a mutating webhook rewriting images.

```sh
k3d cluster create zarf
zarf init --confirm
```

### Zarf's webhook rewrites every image, including the ones you install next

This bites once and is unrecognisable the first time. Zarf's agent rewrites each pod's image to the in-cluster registry, so anything installed afterwards that was not mirrored there fails to pull:

```
Failed to pull image "127.0.0.1:5000/jetstack/cert-manager-controller:v1.21.3-zarf-1554577349"
```

Label the namespaces Zarf should leave alone **before** installing into them, and delete any pod already admitted - the webhook only acts at admission:

```sh
for ns in cert-manager capi-system capd-system; do
  kubectl create namespace "${ns}" --dry-run=client -o yaml | kubectl apply -f -
  kubectl label namespace "${ns}" zarf.dev/agent=ignore
done
```

The same label is what a scratch namespace wants for anything you run by hand. [`../guides/install.md`](../guides/install.md) says this where an operator will read it.

### A k3d node may not reach a public registry as fast as the host does

If a pod sits in `ContainerCreating` on `Pulling image`, pull on the host and hand the image to the cluster rather than waiting:

```sh
docker pull registry.k8s.io/cluster-api/cluster-api-controller:v1.14.4
k3d image import registry.k8s.io/cluster-api/cluster-api-controller:v1.14.4 -c zarf
kubectl delete pod -n capi-system -l cluster.x-k8s.io/provider=cluster-api
```

The deployment's `imagePullPolicy` is `IfNotPresent`, so the imported image is used.

## Installing a build of your branch

The released Zarf package carries released images, so a branch needs its own. Build the images, then point a throwaway edit of `zarf.yaml` at the local daemon:

```sh
make docker-build TAG=0.3.0-dev
```

```yaml
# zarf.yaml, uncommitted: the version and both image tags, plus one line per image
metadata:
  version: 0.3.0-dev
components:
  - name: bootstrap-provider
    images:
      - name: ghcr.io/colonel-byte/cluster-api-cargoship-bootstrap-controller:0.3.0-dev
        source: daemon
```

`source: daemon` is the part worth knowing: without it `zarf package create` resolves every image against a registry and fails with `not found` before any fallback. With it the images come out of the local Docker daemon.

```sh
make zarf-package
zarf package deploy out/zarf-package-cluster-api-provider-cargoship-amd64-0.3.0-dev.tar.zst --confirm
```

Restore `zarf.yaml` and both `config/manager/kustomization.yaml` files afterwards - `make release-manifests` rewrites the latter through `kustomize edit set image`.

To check the air-gapped path actually happened, read the image off the running pod rather than the deployment: Zarf's webhook rewrote it, so a pod serving from `127.0.0.1:5000/...` is the proof.

```sh
kubectl get pod -n cargoship-controlplane-system -o jsonpath='{.items[0].spec.containers[0].image}'
```

## Making the controllers start at all

Both managers watch `clusterv1.Machine`, so without Cluster API's CRDs the manager cannot start its informers. Core Cluster API is not needed for that - the CRDs are:

```sh
make envtest-crds
kubectl apply -f out/envtest-crds/
```

That is enough to create a `Cluster`, a `CargoshipControlPlane` and the `Machine` objects the controller makes, and to watch every requeue. What it cannot do is provision anything, because nothing is running the Cluster API controllers.

## Reaching a step the reconcile guards

The reconcile is a sequence of waits, and each one stops everything after it. To look at a later step, satisfy the earlier ones by hand - which is what the envtest fixture does, and what `kubectl` can do against a real cluster.

| To reach                              | Satisfy                                                                     |
| ------------------------------------- | --------------------------------------------------------------------------- |
| machine creation                      | the `Cluster` owner reference, and `status.initialization.infrastructureProvisioned` |
| the inventory                         | `status.addresses` on each `Machine`                                        |
| the SSH probe                         | an address and port that accept TCP                                         |
| package resolution, the version gate  | all of the above                                                            |

```sh
# Cluster API's own controller writes these two; nothing else will.
kubectl patch cluster fleet --subresource=status --type=merge \
  -p '{"status":{"initialization":{"infrastructureProvisioned":true}}}'
kubectl patch cargoshipcontrolplane fleet --type=json \
  -p "[{\"op\":\"add\",\"path\":\"/metadata/ownerReferences\",\"value\":[{\"apiVersion\":\"cluster.x-k8s.io/v1beta2\",\"kind\":\"Cluster\",\"name\":\"fleet\",\"uid\":\"$(kubectl get cluster fleet -o jsonpath='{.metadata.uid}')\"}]}]"

# An address the probe can dial. The API service answers TCP on 443, so spec.ssh.port: 443 and
# this address get a reconcile past the probe with no host to run.
kubectl patch machine "$(kubectl get machines -o jsonpath='{.items[0].metadata.name}')" \
  --subresource=status --type=merge \
  -p '{"status":{"addresses":[{"type":"InternalIP","address":"10.43.0.1"}]}}'
```

Then read the answer off the conditions rather than the log:

```sh
kubectl get cargoshipcontrolplane fleet \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
```

A converge will not succeed this way - there is no host - but everything up to and including the registry read, the version gate and the fingerprint comparison will.

## Checking the registry read

The package resolution path is the one worth driving against a real registry, because its failures are all about credentials and certificates. Point a `CargoshipControlPlane` at the Zarf registry with `fromZarfState: true` and walk it to the resolution step as above; the condition says what happened:

| Reason                 | Means                                                                       |
| ---------------------- | --------------------------------------------------------------------------- |
| `ZarfStateUnreadable`  | no Zarf deployment, or the controller cannot read its secrets               |
| `ZarfStateUnusable`    | the state carries no credentials                                            |
| `DistroUnresolved`     | the registry answered, and holds no such package or version                 |
| `Converging`           | resolution succeeded and the apply started                                  |

A `DistroUnresolved` naming the versions the registry *does* hold is the read working. The same answer from outside the cluster, which is quicker to iterate on:

```sh
cargoship ls --via-kube                      # tunnels to the service through the API server
cargoship ls --via-kube --name rke2 -o json
```

Note what the records say: a `mirror` record is content a node pulls, and a `package` record is an artifact to install from. A registry holding only the first cannot be converged from, and that is the message the controller gives.

## Checking a ClusterClass

A template is stamped by Cluster API's topology controller, which envtest does not run, so this one needs core Cluster API and an infrastructure provider. The Docker provider does fine here: nothing has to provision, because what is being checked is the stamping.

```sh
version="$(go list -m -f '{{.Version}}' sigs.k8s.io/cluster-api)"
curl -fsSL -o /usr/local/bin/clusterctl \
  "https://github.com/kubernetes-sigs/cluster-api/releases/download/${version}/clusterctl-linux-amd64"
chmod +x /usr/local/bin/clusterctl

CLUSTER_TOPOLOGY=true clusterctl init --core cluster-api --bootstrap - --control-plane - --infrastructure docker
```

`--bootstrap -` and `--control-plane -` install neither, which leaves the provider you deployed with Zarf in place. Confirm the gate reached the manager, since a `ClusterClass` is simply ignored without it:

```sh
kubectl get deploy -n capi-system capi-controller-manager \
  -o jsonpath='{range .spec.template.spec.containers[0].args[*]}{@}{"\n"}{end}' | grep ClusterTopology
```

Then apply a class whose control plane is a `CargoshipControlPlaneTemplate` and whose infrastructure is CAPD's, with a `Cluster` carrying a topology, and read what was stamped:

```sh
kubectl get cargoshipcontrolplane -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{.spec.version}{"\n"}{.spec.replicas}{"\n"}{.spec.machineTemplate.spec.infrastructureRef}{"\n"}{.metadata.annotations}{"\n"}{end}'
```

Three things to look for, because they are the three the template deliberately omits: `spec.version` from the topology, `spec.replicas` from the topology, and `spec.machineTemplate.spec.infrastructureRef` from the class's `machineInfrastructure`. An annotation set under `spec.topology.controlPlane.metadata` should be there too - that is the route an upgrade approval takes.

The worker side is worth one look as well, because the labels differ from a hand-wired `MachineDeployment`:

```sh
kubectl get machinedeployment -o jsonpath='{range .items[*]}{.metadata.name}{"\n  topology="}{.spec.template.metadata.labels.topology\.cluster\.x-k8s\.io/deployment-name}{"\n  capi="}{.spec.template.metadata.labels.cluster\.x-k8s\.io/deployment-name}{"\n"}{end}'
```

The object's name is generated, `topology.cluster.x-k8s.io/deployment-name` holds the class name, and `cluster.x-k8s.io/deployment-name` is absent - which is why the converge reads the first for a host's profile.

## Rendering the clusterctl template

```sh
CLUSTER_CLASS_NAME=cargoship NAMESPACE=default \
CARGOSHIP_REGISTRY=zarf-docker-registry.zarf.svc.cluster.local:5000 \
CARGOSHIP_PACKAGE=k3s CARGOSHIP_PACKAGE_VERSION=1.34.1 \
CARGOSHIP_SSH_USER=rocky CARGOSHIP_SSH_SECRET=fleet-ssh \
CONTROL_PLANE_ENDPOINT_IP=10.0.0.10 \
VSPHERE_SERVER=vc.example VSPHERE_TLS_THUMBPRINT=aa VSPHERE_TEMPLATE=rocky9 \
VSPHERE_DATACENTER=dc VSPHERE_DATASTORE=ds VSPHERE_FOLDER=f \
VSPHERE_RESOURCE_POOL=rp VSPHERE_NETWORK=net \
clusterctl generate cluster fleet-one \
  --kubernetes-version v1.34.1 --control-plane-machine-count 3 --worker-machine-count 2 \
  --from examples/cluster-template-clusterclass-vsphere.yaml
```

clusterctl refuses while any variable is unset and lists the ones it is missing. **It substitutes comments as well as values**, so a comment in a template that mentions a variable by name makes the render fail asking for it - which is how that was found.

## Clearing up

```sh
kubectl delete cluster --all -A                 # before the providers, so finalizers run
kubectl delete ns cc-test cargoship-demo
clusterctl delete --all --include-crd
zarf package remove cluster-api-provider-cargoship --confirm
kubectl delete -f out/envtest-crds/
```

Deleting a `Cluster` while the control-plane controller is gone leaves a finalizer nothing will remove, which is a stuck namespace rather than a lost cluster - delete the clusters first, or patch the finalizer off afterwards.
