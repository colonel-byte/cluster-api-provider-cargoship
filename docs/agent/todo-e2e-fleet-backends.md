# A fleet-backed end-to-end suite, and which backend can carry one

The reconcile state machine is covered: `make test-envtest` drives both controllers against a real API server with the converge stubbed out, and asserts the requeues and conditions rather than any timing. See [e2e-tests](../dev/e2e-tests.md). What is left is the part that needs hosts a converge can actually SSH into, and the backend that can give them is not the one this repository's documentation originally assumed.

This is the plan for that, and the record of why the obvious backend cannot do it.

## What the suite has to arrange

Four things, and only the last two are difficult:

1. **A management cluster.** `kind`, `clusterctl init --core cluster-api`, `make docker-build`, `kind load docker-image`, then the component manifests `make release-manifests` renders. The provider runs as the two pods an operator would run.
2. **Hosts with sshd**, on a network the controller pod can reach.
3. **Machines with addresses.** Something has to play the infrastructure provider: Cluster API's Machine controller copies `status.addresses` off the infrastructure machine, and `converge.BuildInventory` reads nothing else.
4. **A distro package** the pod can pull.

## Tier 2: kind plus bootloose containers

**Bootloose is the backend to use**, not the microVM fleet. Its containers run sshd, each gets a routable Docker address, every one of them answers on port 22, and `config.Machine.Networks` attaches them to a Docker network that already exists - `kind`, so the controller pod and the fleet are on the same segment with no port forwarding anywhere.

`github.com/k0sproject/bootloose` is an ordinary library and already a cargoship dependency. Cargoship's own provisioning for it lives in `test/e2e/cluster/*_test.go`, so it is **not importable**: a test file in another module cannot be linked. Reproducing it is roughly fifty lines against `pkg/config` and `pkg/cluster`, which is cheaper than asking cargoship to export a backend it has no in-tree reason to export. Cargoship's `test/microvm` and root `test` packages stay importable for the hosts this suite cannot get from containers, which is a separate question from the one above.

**The fake infrastructure provider is a CRD and no controller.** One custom resource definition carrying the contract labels, and the test patches `status.addresses` and the ready condition on each object the way a real provider would. This provider needs less of it than most: it copies `spec.machineTemplate.spec.infrastructureRef` verbatim onto every Machine rather than cloning a template (`controlplane/internal/controller/machines.go`), so there is one stub object per machine and no template-to-clone step to imitate.

**The package is what keeps this out of pull-request CI.** The rke2 example cargoship's own suite carries is 2.3GB; k3s is smaller but not small. It has to be in a registry the pod can pull from, and `converge.SetPaths` needs a volume with room for an extracted copy. That makes this a nightly-or-manual `make test-e2e`, behind its own build tag, rather than something every pull request pays for. Say so in the target's help text: a suite that is quietly never run is worse than one that is explicitly manual.

Two things to spike before committing to this, each about twenty minutes: that bootloose's `Networks` really does land containers on an existing `kind` network, and that a controller-less stub CRD satisfies the Cluster API v1beta2 infrastructure-machine contract well enough for the Machine controller to copy addresses off it.

## Tier 3: the microVM fleet, and why not

[`docs/dev/microvm.md`](https://github.com/colonel-byte/cargoship/blob/main/docs/dev/microvm.md) in cargoship, and cargoship's own `choice-capi-provider.md`, both point this provider's e2e suite at the microVM fleet, on the grounds that CAPD containers run no sshd. That reasoning is sound and the conclusion does not follow: bootloose containers *do* run sshd, and CAPD was never the only container option.

The fleet cannot carry this suite, for three reasons, any one of which is enough:

- **Each node is reachable only as `127.0.0.1:<its own forwarded port>`.** The nodes are qemu user-mode networking (`test/microvm/vm.go`, `hostfwd=tcp:127.0.0.1:<SSHPort>-:22`), one host port per node. `converge.SSHParams.Port` is one port for the whole inventory, and a Cluster API `MachineAddress` carries no port at all, so a fleet of per-node ports is not expressible.
- **The fleet's own layer 2 is a qemu socket on a multicast group.** The nodes' `10.73.x` addresses work between nodes and are not routable from the host, let alone from a pod.
- **A pod in kind cannot reach the host's loopback** regardless of the above.

Two things would lift the first point, and neither is worth doing for a test:

- **A per-machine SSH port**, read by `BuildInventory` off a Machine annotation. Ten lines, and arguably useful beyond tests - NAT'd fleets with per-host forwarded ports are a real arrangement. It would let a test binary *running on the host* converge a microVM fleet. It would still not let a pod do it, so it buys a different suite than the one this document is about.
- **A bridged or tap mode in cargoship's microvm package.** That needs root or `CAP_NET_ADMIN`, which is exactly the promise that package makes a point of keeping ("needs no root, starts no daemon, changes no setting on your machine"). Do not ask for it.

What the fleet is for is the SELinux, fapolicyd and firewalld phases, which do nothing in a container. Those are cargoship's phases, cargoship's suite already runs them against the fleet, and nothing about this provider changes what they do.

## What no backend here covers

**Neither fleet ever consumes this provider's bootstrap data.** Containers ignore cloud-init entirely - bootloose authorizes the key itself, through `config.Machine.PublicKey` - and the microVM fleet writes its own seed. So `bootstrap/internal/cloudinit` is tested by its unit tests and by the envtest assertions on the rendered secret, and the first thing that proves a machine booted reachable *because of us* is a real infrastructure provider: vSphere, OpenStack, anything with a cloud-init image. That is worth stating plainly, because a green tier 2 run looks like it covers it and does not.
