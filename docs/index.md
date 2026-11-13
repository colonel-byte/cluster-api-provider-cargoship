# cluster-api-provider-cargoship

A Cluster API bootstrap and control-plane provider that stands up Kubernetes with [cargoship](https://github.com/colonel-byte/cargoship) instead of kubeadm.

Cargoship builds offline K3s, RKE2 and upstream Kubernetes packages and installs them over SSH from a management node. This provider does the same thing from a management *cluster*: it creates the `Machine` objects, waits for an infrastructure provider to give them addresses, and then converges the whole fleet - control plane and workers together - with one `cargoship apply`. Rancher Turtles imports the result like any other Cluster API cluster, with no cargoship-specific configuration on the Turtles side.

## Why not a timer

Every infrastructure provider takes its own unpredictable time to produce a machine with an address, and the Kubernetes API then takes its own unpredictable time after that. A design with a timeout in it is wrong for at least one provider.

There are no timers here. Cargoship's `apply` is a converge rather than an install - each phase decides per host whether it has anything to do - so every reconcile rebuilds the host list from **whatever machines currently have an address**, converges those, and lets the next reconcile pick up the ones that arrived late. A machine that boots in 40 seconds and one that boots in 11 minutes take the same code path, and nothing anywhere gives up because something took too long.

The one long operation is the cargoship apply itself, which walks every host over SSH and takes minutes. It cannot be suspended and resumed, so the reconcile does not call it directly: it starts at most one converge per cluster and the reconciles that follow observe the one that is running. Everything else - an address appearing, SSH beginning to answer, the API server beginning to answer - is an ordinary requeue.

## Installation

```sh
clusterctl init \
  --infrastructure vsphere \
  --bootstrap cargoship \
  --control-plane cargoship
```

`clusterctl` needs to know where the provider lives until it is in the built-in list. Add this to `~/.config/cluster-api/clusterctl.yaml`:

```yaml
providers:
  - name: cargoship
    url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases/latest/bootstrap-components.yaml
    type: BootstrapProvider
  - name: cargoship
    url: https://github.com/colonel-byte/cluster-api-provider-cargoship/releases/latest/control-plane-components.yaml
    type: ControlPlaneProvider
```

## Usage

The SSH key the controller connects with, and the only secret this provider needs:

```sh
ssh-keygen -t ed25519 -N '' -f cluster-key
kubectl create secret generic fleet-ssh \
  --type=kubernetes.io/ssh-auth \
  --from-file=ssh-privatekey=cluster-key
```

The key is read into memory and handed to the SSH client as an auth method. It is never written to the controller's filesystem - the bootstrap provider derives the public half from the same secret and authorizes it on every machine, so there is no second key to keep in step.

A control plane:

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1alpha1
kind: CargoshipControlPlane
metadata:
  name: fleet
spec:
  replicas: 3
  version: v1.34.1
  distro:
    # Pin to a digest. A tag that is republished underneath a running cluster is not picked up;
    # see docs/agent/choice-distro-pinning.md.
    ociRef: oci://ghcr.io/example/k3s@sha256:1f5c...
    arch: amd64
  ssh:
    user: rocky
    privateKeySecretRef:
      name: fleet-ssh
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: VSphereMachineTemplate
        name: fleet-cp
  clusterConfig:
    # cargoship's own spec.config, verbatim: registries, profiles, values.
    values:
      cni: cilium
    profiles:
      gpu:
        engine:
          labels:
            nvidia.com/gpu: "true"
  apply:
    modifyHosts: true
    modifyFirewall: true
    labelNodes: true
```

Workers come from an ordinary `MachineDeployment` pointing at a `CargoshipConfigTemplate`. **The deployment's name is the cargoship profile**, so the `gpu` deployment below picks up the `gpu` profile above - its node labels, taints, firewall rules and concurrency - with nothing else to wire together.

## How it differs from KubeadmControlPlane

| | KubeadmControlPlane | CargoshipControlPlane |
| --------------------- | ------------------------------------------ | ---------------------------------------------------------- |
| Install mechanism     | cloud-init on each machine                 | SSH from the management cluster |
| Bootstrap data        | the whole install                          | an authorized key, so cargoship can connect |
| Scope                 | control-plane machines                     | the whole fleet, in one converge |
| Worker joins          | a separate bootstrap provider per machine  | the same converge |
| Node removal          | drain and delete                           | drain, delete, then re-converge the survivors |

The scope row is the real difference and the reason for the design: a cargoship fleet shares an `/etc/hosts` file and a firewall across every host, so two uncoordinated SSH walks over it would fight. See [`docs/agent/choice-one-converge.md`](agent/choice-one-converge.md).

The node-removal row is the second. A machine Cluster API is destroying needs no `cargoship reset` - but the machines that remain need their `/etc/hosts` and firewall rules rewritten without it, and a departing control-plane member needs to leave etcd first. Both are handled with a Cluster API pre-terminate hook: Cluster API drains the node and deletes the `Node` object, then blocks the infrastructure provider from destroying the instance until the controller releases the hook. The controller converges the survivors in that window and then lets the instance go.

## Not yet supported

Stated plainly, because each of these is a thing an operator will reach for:

- **No rollout or in-place upgrade.** Scale up and scale down only. A change to `spec.version` is rejected rather than half-applied. Cargoship has upgrade phases; wiring them to a Cluster API rollout is its own piece of work.
- **No tag-to-digest resolution.** A republished tag is not noticed. Pin the reference.
- **No encrypted registry credentials.** Cargoship's keyring type is not reachable from outside its module.
- **No MachineHealthCheck remediation**, etcd backup or restore.

## Development

```sh
make generate         # CRDs, RBAC and deepcopy functions
make test             # unit tests, with the race detector
make verify           # fail if a generator would change a checked-in file
make release-manifests  # the three files clusterctl downloads
make zarf-package     # the air-gapped Zarf package, built from those manifests
```

The repository vendors its dependencies, so everything above works with no network access - apart from `make zarf-package`, which pulls the published controller images. See [`docs/dev/e2e-tests.md`](dev/e2e-tests.md) for the end-to-end suite, which drives a real fleet of virtual machines - the Docker infrastructure provider's containers run no sshd, so they cannot be used to test a converge.

## License

Apache 2.0. See [`LICENSE`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/LICENSE).
