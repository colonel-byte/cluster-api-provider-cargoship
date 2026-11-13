# Pulling the distro package from a Zarf registry

The control-plane controller pulls `spec.distro.ociRef` itself, from inside the management cluster, every time it converges a fleet. A Zarf registry is the usual place for that package in an air gap, and it is the awkward one: it wants credentials, and it serves a certificate nothing in the controller trusts. Both are handled on the `CargoshipControlPlane`.

```yaml
spec:
  distro:
    ociRef: oci://zarf-docker-registry.zarf.svc.cluster.local:5000/example/k3s@sha256:1f5c...
    pullSecretRef:
      name: zarf-registry
    insecureSkipTLSVerify: true
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

`insecureSkipTLSVerify: true` is the per-cluster answer, and it means what it says: the registry's certificate is accepted without being checked. It covers the package pull only - the SSH connections to the fleet and the workload cluster's API server are not affected by it.

Trusting the certificate properly is better where one registry serves every cluster, and it is a property of the controller rather than of a `CargoshipControlPlane`. Mount the CA into the control-plane controller's deployment and point `SSL_CERT_FILE` at it:

```yaml
        env:
          - name: SSL_CERT_FILE
            value: /etc/cargoship/ca/registry-ca.crt
        volumeMounts:
          - name: registry-ca
            mountPath: /etc/cargoship/ca
            readOnly: true
      volumes:
        - name: registry-ca
          secret:
            secretName: registry-ca
```

`SSL_CERT_FILE` replaces the list of certificate *files* Go would otherwise try, but the certificate *directories* are still read - `/etc/ssl/certs` among them - so the public roots the image ships are kept and the mounted CA is added to them. Setting `SSL_CERT_DIR` instead is what would replace them.

What cannot be done at all: a registry configured for mTLS, where the client has to present a certificate of its own. Nothing on the pull path accepts a client certificate, so a Zarf registry with `mtlsStrategy` set to anything requiring one cannot be pulled from by this controller.

## Why this is not the Zarf package's problem

[The Zarf package](install.md#installing-without-a-route-to-ghcrio) installs the provider. The distro package is what the provider installs onto the fleet, it is pulled at converge time rather than at install time, and it is normally much larger - so it stays in the registry and the two are mirrored separately.
