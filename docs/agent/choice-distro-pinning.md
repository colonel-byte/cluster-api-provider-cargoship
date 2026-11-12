# A tagged distro reference is not re-resolved, and a republished tag is not noticed

`spec.distro.ociRef` is used exactly as given. The desired-state fingerprint hashes the reference string, so a package rebuilt and republished behind the same tag produces the same fingerprint, and no converge runs. **Pin the reference to a digest where that matters.**

This is a limitation of the current version rather than a decision, and it is written down because the obvious fix looks available and is not.

## Why it is not fixed yet

The primitive is nearly there. `coci.Resolve` returns a `Resolution` whose `Reference` field is already the reference with its tag replaced by the resolved digest - exactly what is wanted. But it takes a `FileSelector`, and for a manifest carrying more than one layer it returns `ErrLayerRequired`:

```go
// pkg/coci/file.go
var ErrLayerRequired = errors.New("no layer title was selected")
```

A distro package carries many layers (`metadata`, `images`, `os`, `files` - see `pkg/coci/layers`), so calling `Resolve` with an empty selector fails. `Resolve` is built for per-file artifacts, which is what its callers in cargoship are.

What is missing is a cargoship-side "resolve this reference's root digest, no selector" - the resolution step `coci.Resolve` already does internally before it goes looking for a layer. That is a change in `colonel-byte/cargoship`, not here, and it is a one-function addition rather than anything structural.

## What this does and does not break

- **It does not** stop a new package being adopted. Change `spec.distro.ociRef` and the fingerprint changes. A new tag or a digest both work.
- **It does** mean `status.distro.digest` is empty for a tagged reference, so there is no record of what a fleet actually converged to. An operator who needs that answer has to pin.
- **It does not** affect correctness of a converge. Cargoship resolves the reference itself on every apply, so the bytes installed are always whatever the tag points at now. The gap is only in *noticing* that it changed.

Do not work around this by folding a timestamp or the reconcile count into the fingerprint. That would make the fingerprint change on every pass, which turns the drift interval into "re-converge the entire fleet over SSH every ten minutes" - the cost control the fingerprint exists to provide, removed.
