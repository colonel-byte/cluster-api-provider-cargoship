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
