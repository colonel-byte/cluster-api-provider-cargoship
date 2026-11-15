# Security Policy

## Supported versions

`cluster-api-provider-cargoship` is pre-1.0 and releases from `main` on every conventional commit that warrants one, so fixes land in a new patch or minor release rather than being backported. Only the most recent release line receives security fixes; upgrade before reporting an issue against an older tag.

| Version               | Supported |
| --------------------- | --------- |
| Latest `v0.x` release | Yes       |
| Any earlier release   | No        |

## Reporting a vulnerability

Report privately through GitHub: **[open a draft advisory](https://github.com/colonel-byte/cluster-api-provider-cargoship/security/advisories/new)**. Do not open a public issue, and do not include a working exploit against a third party's cluster.

You should expect an acknowledgement within 3 business days and an assessment -- whether we consider it a vulnerability, and the severity we assign -- within 10 business days. Confirmed issues are fixed in the next release, and we aim to publish the advisory within 90 days of the report, sooner when a fix is already out. You will be credited in the advisory unless you ask not to be.

Include the release of this provider, the Cluster API version in the management cluster, the infrastructure provider in use, the `CargoshipConfig` or `CargoshipControlPlane` that reproduces the behaviour with its secrets removed, and what an attacker gains.

## Scope

This provider is unusual in a way that matters for a security report: it runs **in** the management cluster and opens SSH connections out to the fleet, rather than handing a machine user-data to bootstrap itself with. The controller therefore holds an SSH key that can reach every host in the cluster, and anything that leaks that key or redirects where it connects is in scope.

In scope: the two managers and the packages under the module root; the controller's handling of the SSH key, which is read from a secret and kept in memory only ([`choice-in-memory-ssh-key`](../docs/agent/choice-in-memory-ssh-key.md)); the registry credentials and CA material the control plane reads out of a zarf state secret; the cloud-init the bootstrap provider renders into a data secret; the RBAC each manager ships with; and the release workflow and the artifacts it publishes, including the component manifests, the OCI artifact and the Zarf package.

Out of scope: vulnerabilities in [cargoship](https://github.com/colonel-byte/cargoship) itself, which this provider consumes as a library -- report those to that repository, which has its own [policy](https://github.com/colonel-byte/cargoship/blob/main/.github/SECURITY.md); vulnerabilities in K3s, RKE2, containerd, Cluster API or any other upstream component -- report those to the project that owns them; anything that requires an attacker to already hold the controller's service account or read access to its namespace's secrets, which by design hold the cluster's credentials; and misconfiguration of a user's own cluster or fleet.

## Verifying a release

The component manifests' checksums, the manager binaries and the controller images are signed with [cosign](https://docs.sigstore.dev/cosign/) using the key whose public half is [`cosign.pub`](../cosign.pub) in this repository, and archives ship with an SBOM.

```console
cosign verify-blob --key cosign.pub --bundle checksums.txt.sigstore.json checksums.txt
cosign verify --key cosign.pub ghcr.io/colonel-byte/cluster-api-cargoship-bootstrap-controller:<tag>
cosign verify --key cosign.pub ghcr.io/colonel-byte/cluster-api-cargoship-controlplane-controller:<tag>
```

Every release also publishes build provenance for the archives, the images and `ai-bom.json` as GitHub attestations, verified with `gh attestation verify <file> --repo colonel-byte/cluster-api-provider-cargoship`.

## AI-assisted development

This repository's code, docs and design records are written with AI coding agents in the loop; [`AGENTS.md`](../AGENTS.md) is the instruction set they work from. Each release publishes `ai-bom.json`, a per-release snapshot of the disclosure in [`.github/ai-bom.json`](ai-bom.json), attested the same way as the other release artifacts, [above](#verifying-a-release).

## Security in CI

Pull requests and `main` are scanned by CodeQL, dependencies are updated by Dependabot, Go advisories are checked for reachability by `govulncheck` and the controller images by trivy ([`scan-vuln.yaml`](workflows/scan-vuln.yaml)), and supply-chain practices are scored weekly by OpenSSF Scorecard. Results appear in the repository's Security tab. `vendor/` is committed so the repository builds with no network access, which costs points on a couple of Scorecard checks and reports advisories that are not this module's; [`choice-vendor-lockfile-prune`](../docs/agent/choice-vendor-lockfile-prune.md) explains what is done about it and what is left.
