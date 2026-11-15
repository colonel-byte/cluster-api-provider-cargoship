# Registry credentials are written to a file, and the SSH key is not

[`choice-in-memory-ssh-key.md`](choice-in-memory-ssh-key.md) is emphatic that the SSH key never reaches a filesystem. The registry credentials in `spec.distro.pullSecretRef` do: the controller writes them into a Docker config file on its cache volume. The difference is not carelessness, and the file is not a fallback for something better.

## There is no in-process credential hook to use

Cargoship's `distro.Load` builds the oras modifier list itself, inside `pullOCI`, from the `RemoteOptions` it was given - `PlainHTTP` and `InsecureSkipTLSVerify`, and nothing else. `oci.WithCredentialFunc` exists upstream in zarf's OCI package and is exactly the right hook, but a caller cannot reach it through `LoadOptions`. What is left is the fallback oras takes when no credential function is set: the Docker credential store, read from `$DOCKER_CONFIG/config.json` each time a remote is built.

So the choice was a file or a cargoship change, and the file is reversible in an afternoon once `LoadOptions` can carry a credential function. The SSH key had no such constraint - rig's `AuthMethods` takes the bytes - which is why the two are different.

## One file, merged, rather than one per resource

`$DOCKER_CONFIG` is a process-wide variable, and `converge.Runner` holds one in-flight converge *per resource*, so two clusters converge at once. A file per `CargoshipControlPlane` would mean rewriting the variable underneath a pull that is already reading it.

`pkg/dockerconfig.Merge` writes one file instead, keyed by registry, under a mutex and through a rename. Two clusters pulling from two registries each find their own entry. Two naming the same registry with different credentials are the case this does not serve: the last converge to start wins, which is recorded as a `shortcut:` comment beside the merge rather than hidden.

## `fromZarfState` is a second source for the same file

`spec.distro.fromZarfState` reads the credentials out of the Zarf deployment in the controller's own cluster instead of out of a secret an operator wrote, and it changes nothing about the above: `zarfstate.Load` hands back a username and a password, `dockerconfig.MergeCredential` assembles the one-registry payload, and the pull reads the same file it always did. The listing that resolves a package by name reads it too - `distro.ListMirroredOptions` falls back to the Docker credential store when handed no username - so neither half has to be threaded through a function signature.

What it saves is the two secrets, which were the air-gapped operator's only remaining manual step: `zarf tools get-creds`, a `kubectl create secret docker-registry` whose `--docker-server` had to match the registry string exactly, and the registry CA extracted out of `zarf-registry-server-tls` by hand. Both are already in the cluster, and `cargoship mirror --from-zarf-state` already read them - which is why [cargoship 0.42.1 exports `pkg/zarfstate`](https://github.com/colonel-byte/cargoship/pull/1118) rather than this repository decoding zarf's state JSON a second time.

**The registry address is not read from that state, and that is the part worth knowing.** Zarf records where a *node* reaches the registry - a NodePort on the host, or an external address - and a controller inside the cluster wants the service, which the state does not carry at all; `cargoship mirror` holds `zarf/zarf-docker-registry:5000` as a constant for the same reason. So `spec.distro.registry` is still named by the operator, and `fromZarfState` requires it: the Docker credential store is keyed by the host a pull asks for, so there has to be one to key the entry under.

## A Zarf-managed-mTLS registry needs no client certificate, measured rather than assumed

`zarfstate.Registry.UsesMTLS()` reports true for any `mtlsStrategy` other than `none`, and the obvious reading - that such a registry cannot be pulled from without a client certificate, which nothing here presents - is wrong. It was implemented that way first, as a refused reconcile.

Zarf's registry chart sets the client-auth mode to `verify-client-cert-if-given`:

```
# packages/init/v1alpha1/zarf-registry/chart/templates/_helpers.tpl
"clientcas" (list "/certs/server/ca.crt")
"clientauth" "verify-client-cert-if-given"
```

That is `tls.VerifyClientCertIfGiven`: a certificate is verified where the client offers one, and a client that offers none is still served. The certificate Zarf issues - `zarf-registry-client-tls`, common name `zarf-registry-client` - is mounted into the per-node registry proxy, which is a `socat` presenting it on a node's behalf, and not into anything a management-cluster controller runs. So the CA out of `zarf-registry-server-tls` is the whole requirement, and `UsesMTLS()` is logged at `V(1)` rather than acted on.

The case this does not cover is a registry that genuinely requires one - Harbor or Nexus configured for it, or a Zarf registry whose `clientauth` an operator overrode through `secrets.configData`, which the chart's `mergeOverwrite` allows. That needs a client certificate on the pull, and there is nowhere to put one: `images.ClientOptions` carries `CABundle` and builds the `tls.Config` itself, with no `Certificates` and no transport to substitute, and `distro.LoadOptions` and `distro.ListMirroredOptions` pass their own `CABundle` down the same path. Supporting it is a cargoship change first - a client certificate beside `CABundle` in those three option structs - and a `clientCertSecretRef` here second.

## The blast radius is smaller than the SSH key's

Worth saying plainly, because "a secret on a disk" is where review attention goes. The SSH key opens a root shell on every machine in the fleet; these credentials pull a public artifact from a registry, and Zarf's `zarf-pull` user cannot even push to it. The file is `0600` on a volume only the controller mounts, in a pod running as `65532` with no shell in the image.
