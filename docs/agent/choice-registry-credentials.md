# Registry credentials are written to a file, and the SSH key is not

[`choice-in-memory-ssh-key.md`](choice-in-memory-ssh-key.md) is emphatic that the SSH key never reaches a filesystem. The registry credentials in `spec.distro.pullSecretRef` do: the controller writes them into a Docker config file on its cache volume. The difference is not carelessness, and the file is not a fallback for something better.

## There is no in-process credential hook to use

Cargoship's `distro.Load` builds the oras modifier list itself, inside `pullOCI`, from the `RemoteOptions` it was given - `PlainHTTP` and `InsecureSkipTLSVerify`, and nothing else. `oci.WithCredentialFunc` exists upstream in zarf's OCI package and is exactly the right hook, but a caller cannot reach it through `LoadOptions`. What is left is the fallback oras takes when no credential function is set: the Docker credential store, read from `$DOCKER_CONFIG/config.json` each time a remote is built.

So the choice was a file or a cargoship change, and the file is reversible in an afternoon once `LoadOptions` can carry a credential function. The SSH key had no such constraint - rig's `AuthMethods` takes the bytes - which is why the two are different.

## One file, merged, rather than one per resource

`$DOCKER_CONFIG` is a process-wide variable, and `converge.Runner` holds one in-flight converge *per resource*, so two clusters converge at once. A file per `CargoshipControlPlane` would mean rewriting the variable underneath a pull that is already reading it.

`pkg/dockerconfig.Merge` writes one file instead, keyed by registry, under a mutex and through a rename. Two clusters pulling from two registries each find their own entry. Two naming the same registry with different credentials are the case this does not serve: the last converge to start wins, which is recorded as a `shortcut:` comment beside the merge rather than hidden.

## The blast radius is smaller than the SSH key's

Worth saying plainly, because "a secret on a disk" is where review attention goes. The SSH key opens a root shell on every machine in the fleet; these credentials pull a public artifact from a registry, and Zarf's `zarf-pull` user cannot even push to it. The file is `0600` on a volume only the controller mounts, in a pod running as `65532` with no shell in the image.
