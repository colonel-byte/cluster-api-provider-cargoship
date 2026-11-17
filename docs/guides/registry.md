# Pulling the distro package from a Zarf registry

The control-plane controller pulls `spec.distro.ociRef` itself, from inside the management cluster, every time it converges a fleet. A Zarf registry is the usual place for that package in an air gap, and it is the awkward one: it wants credentials, and it serves a certificate nothing in the controller trusts. Both are handled on the `CargoshipControlPlane`.

```yaml
spec:
  distro:
    ociRef: oci://zarf-docker-registry.zarf.svc.cluster.local:5000/example/k3s@sha256:1f5c...
    pullSecretRef:
      name: zarf-registry
    caBundleSecretRef:
      name: zarf-registry-ca
```

## The credentials

A `kubernetes.io/dockerconfigjson` secret, in the same namespace as the `CargoshipControlPlane` - the controller reads it from there and nowhere else. Zarf's pull-only user is `zarf-pull`, and its password comes out of the cluster rather than out of a notebook:

```sh
kubectl create secret docker-registry zarf-registry \
  --namespace default \
  --docker-server=zarf-docker-registry.zarf.svc.cluster.local:5000 \
  --docker-username=zarf-pull \
  --docker-password="$(zarf tools get-creds registry-readonly)"
```

**`--docker-server` must match the registry in `ociRef` exactly**, port included. The credential store is keyed by that string, so `zarf-docker-registry.zarf.svc.cluster.local:5000` and `zarf-docker-registry.zarf.svc.cluster.local` are two different registries as far as the pull is concerned, and the second one has no credentials.

The controller merges the secret's registries into a Docker config file on its cache volume, because that file is the only thing the pull reads credentials from - there is no in-process credential hook to use instead. One file is shared by every `CargoshipControlPlane` and merged per registry, so two clusters pulling from two registries coexist; two naming the same registry with different credentials do not, and the last converge to start wins. See [the decision record](../agent/choice-registry-credentials.md).

A pull secret that is missing, or is not a `dockerconfigjson` secret, fails the reconcile rather than quietly falling back to anonymous access. `kubectl describe cargoshipcontrolplane` reports it on the `Converged` condition with the reason `PullSecretUnusable`.

## The certificate

The registry serves a certificate nothing public signed, so hand the controller the CA that signed it. Where that CA comes from depends on how the registry was stood up - Zarf generates one when it manages the registry's TLS, and an external registry has whatever PKI issued it - but it reaches the controller the same way either way, in a secret's `ca.crt` key:

```sh
kubectl create secret generic zarf-registry-ca \
  --namespace default \
  --from-file=ca.crt=registry-ca.crt
```

```yaml
spec:
  distro:
    caBundleSecretRef:
      name: zarf-registry-ca
```

Three things about it:

- **The key must be `ca.crt`**, and the value must be one or more PEM certificates. Any other key is rejected on the `Converged` condition with the reason `CABundleUnusable`, rather than falling back to the public roots and failing the pull minutes later with `certificate signed by unknown authority`.
- **It is trusted in addition to the controller's own roots**, not instead of them. A cluster pulling its distro package from a private registry still reaches anything publicly signed.
- **The secret lives in the `CargoshipControlPlane`'s namespace**, like the pull secret and the SSH key.

`insecureSkipTLSVerify: true` is the other option, and it is a different thing: it accepts whatever certificate the registry presents without checking it. Reach for it to prove a registry is otherwise reachable, not to run a fleet. It covers the package pull only - the SSH connections and the workload cluster's API server are not affected by either field.

What cannot be done at all: a registry configured for mTLS, where the client has to present a certificate of its own. Nothing on the pull path accepts a client certificate, so a Zarf registry with `mtlsStrategy` set to anything requiring one cannot be pulled from by this controller.

## Why this is not the Zarf package's problem

[The Zarf package](install.md#installing-without-a-route-to-ghcrio) installs the provider. The distro package is what the provider installs onto the fleet, it is pulled at converge time rather than at install time, and it is normally much larger - so it stays in the registry and the two are mirrored separately.

## Letting Zarf's own state answer both

A registry Zarf stood up has already generated the credentials and signed the certificate, and both are in the management cluster as the `zarf-state` and `zarf-registry-server-tls` secrets. `fromZarfState: true` reads them from there, so neither secret above has to be created:

```yaml
spec:
  distro:
    fromZarfState: true
    registry: zarf-docker-registry.zarf.svc.cluster.local:5000
    name: k3s
    packageVersion: 1.34.1
```

That is the whole configuration for an air gap - no `pullSecretRef`, no `caBundleSecretRef`, and the API server refuses the resource if either is set alongside it, rather than leaving two answers to one question.

Four things about it:

- **`registry` is still required.** Zarf records the address a *node* reaches the registry at, which is a NodePort or an external host; a controller inside the cluster wants the service, and the state does not carry it. The credential entry is keyed by whatever is written here, so it has to be the host a pull will ask for.
- **The pull-only credentials are used**, falling back to the push pair only if the state carries no pull user. Zarf writes both.
- **A missing CA is not an error.** `zarf-registry-server-tls` exists where Zarf manages the registry's TLS; an external registry serving a publicly signed certificate has none, and there is nothing extra to trust.
- **A registry Zarf manages mTLS for still works**, with that CA and no client certificate of the controller's own. Zarf configures the registry `clientauth: verify-client-cert-if-given`, so a certificate is verified where one is offered and never demanded - the one Zarf issues belongs to the per-node registry proxy, which presents it for a node. The conditions to expect here are `ZarfStateUnreadable` for a cluster with no Zarf deployment in it, `ZarfStateUnusable` for state that carries no credentials, and `DistroUnresolved` for a package the registry does not hold.

The controller reads both secrets with its own service account, through the `ClusterRole` it already carries; nothing extra is granted for this and the resource does not say which namespace to look in - `zarf` is where Zarf keeps them.

## Naming the package instead of referencing it

A registry filled by cargoship can be asked what it holds, which saves writing a reference that has to be right. Set `registry`, `name` and `packageVersion` in place of `ociRef`:

```yaml
spec:
  distro:
    registry: zarf-docker-registry.zarf.svc.cluster.local:5000
    name: k3s
    packageVersion: 1.34.1
    pullSecretRef:
      name: zarf-registry
    caBundleSecretRef:
      name: zarf-registry-ca
```

The credentials and the certificate are the same two secrets and the same two conditions as above; `--docker-server` must match `registry` exactly, port included, for the same reason it had to match the reference.

This reads the records `cargoship publish --record` and `cargoship mirror` leave in `<registry>/cargoship`, so it works against a registry those filled and reports nothing for one they did not. The package itself has to have been **published** into the registry, not only mirrored into it: a mirror pushes the images and files a node will ask for, which is the other half of an air gap and is not an artifact this controller can pull. A registry holding only the mirrored half says so:

```
zarf-docker-registry.zarf.svc.cluster.local:5000: k3s is recorded as mirrored into this registry but not published into it, so there is no package to pull: publish it with `cargoship publish --record`
```

A `packageVersion` nothing recorded lists what is on offer instead, on the `Converged` condition with the reason `DistroUnresolved`:

```
zarf-docker-registry.zarf.svc.cluster.local:5000: no record of k3s 1.37.0; recorded versions are 1.34.1, 1.36.1
```

What it does report, for a package it resolved, is the rest of that package's published versions:

```sh
kubectl get cargoshipcontrolplane fleet \
  -o jsonpath='{.status.distro.version} -> {.status.distro.availableVersions}'
```

That is advertisement and nothing else - no version here moves a cluster. Taking one is a change to `spec.distro.packageVersion` and `spec.version` together, plus the `controlplane.cluster.x-k8s.io/approve-version-change` annotation, because an in-place upgrade of every host is not something a registry gets to start. `status.distro.engine` and `status.distro.version` are filled from the same record.

Two things it deliberately does not do. **It does not take the newest version** when `packageVersion` is left out - the field is required alongside `name`, so publishing a package never moves a cluster. And **it does not follow a republished version** unless asked: the registry's content digest for the resolved package lands in `status.distro.recordDigest` either way, but it only joins the desired state, and so only triggers a converge, with

```sh
kubectl annotate cargoshipcontrolplane fleet \
  controlplane.cluster.x-k8s.io/adopt-republished-package=true
```

Both are explained in [the decision record](../agent/choice-record-discovery.md).

Written once per cluster *shape* rather than once per cluster, if you compose a `ClusterClass`: the four fields above live in the `CargoshipControlPlaneTemplate`'s `spec.template.spec.distro`, and every cluster built from that class inherits them. A package version that differs per cluster is a topology variable with a patch onto `/spec/template/spec/distro/packageVersion`. See [Composing a ClusterClass](clusterclass.md).

## A secret in another namespace

`pullSecretRef`, `caBundleSecretRef` and `ssh.privateKeySecretRef` each take an optional `namespace`. Empty - and anything written before the field existed - means the namespace the `CargoshipControlPlane` itself is in, which is the only thing the controller allows by default:

```yaml
spec:
  distro:
    pullSecretRef:
      name: zarf-registry
      namespace: zarf
```

A reference to any other namespace is refused on the reconcile unless the control-plane *and* bootstrap controllers were both started with `--secret-namespaces` naming it:

```sh
# Both managers, and the same list: one credential, read by two controllers.
--secret-namespaces=zarf,shared-creds
# Or, if every namespace is as trusted as every other:
--secret-namespaces='*'
```

Refusing by default is deliberate rather than conservative. The controller reads secrets with a cluster-wide `ClusterRole`, so anyone who can create a `CargoshipControlPlane` in their own namespace could otherwise point it at a credential they have no access to - and for the SSH key that is worse than reading it, because the bootstrap provider derives the public half and installs it on their machines. See [the decision record](../agent/choice-cross-namespace-secrets.md).

Two limits to know before using it:

- **`clusterctl move` does not move the secret.** It moves the objects the cluster owns, and a secret in a namespace that is not the cluster's is not one of them. Move it yourself, or the cluster arrives in the target management cluster unable to reach its registry or its fleet.
- **A manager running with `--namespace` cannot see it.** That flag scopes the whole cache to one namespace, so a secret anywhere else reads as not found. Nothing checks the two flags against each other at startup; the symptom is a `NotFound` naming a secret that exists.
