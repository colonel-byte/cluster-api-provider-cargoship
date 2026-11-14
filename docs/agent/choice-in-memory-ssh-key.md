# The SSH key is never written to a file, and nothing had to be added to make that true

The controller holds a cluster-wide SSH private key - the single most valuable thing in the management cluster, because it opens a root-capable shell on every node of every cluster it manages. It is read from a `kubernetes.io/ssh-auth` secret, parsed into an in-memory auth method, and handed to rig. It does not touch a filesystem, there is no volume for it, and no `emptyDir` to shred.

This looks like it should have required work, because rig's SSH configuration takes a *path*:

```go
// vendor/github.com/k0sproject/rig/v2/protocol/ssh/config.go
KeyPath *string `yaml:"keyPath,omitempty" ...`
```

It did not. The same struct carries a field that is excluded from both YAML and JSON, and so appears in no generated schema and in no example anybody has read:

```go
AuthMethods []ssh.AuthMethod `yaml:"-" json:"-"`
```

When it is set, rig's connection setup short-circuits the entire identity-file path (`protocol/ssh/connection.go`, `if len(c.AuthMethods) > 0 { config.Auth = c.AuthMethods }`), and `ssh.ParseSSHPrivateKey(keyBytes, nil)` is the helper that builds it. `pkg/converge.BuildInventory` sets it per host.

## Do not add an alternative

Three things that look like improvements and are not:

- **Materializing the key to an `emptyDir`.** This is what the field above exists to avoid. It puts cluster-admin-equivalent credentials on a filesystem, where a sidecar, a debug container, a core dump or a misconfigured volume mount can read them.
- **An `ssh-agent` sidecar.** More moving parts, a socket in a shared volume, and no security gain over a key that is already only in process memory.
- **An exported "give cargoship a key as bytes" hook in `colonel-byte/cargoship`.** It would be a second way to do what rig already does, and it would have to be kept in step with rig's auth handling.

The one thing worth doing is the thing already done: `pkg/sshkey.AuthorizedKey` *derives* the public half from the same secret rather than taking it separately, so there is no way for the authorized key and the connecting key to drift apart. A mismatched pair produces a fleet of machines that boot correctly, report ready, and refuse every connection - a failure that looks like a network problem for as long as it takes somebody to compare two keys by hand.

Encrypted private keys are refused rather than prompted for. There is no terminal here, and a passphrase the controller cannot supply would make every machine unreachable.
