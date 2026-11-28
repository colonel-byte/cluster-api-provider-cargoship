# Vulnerability scan

Source: [`scan-vuln.yaml`](https://github.com/colonel-byte/cluster-api-provider-cargoship/blob/main/.github/workflows/scan-vuln.yaml). Runs on every pull request, in the merge queue, and weekly on a schedule, with in-progress runs cancelled per ref.

`govulncheck` reports Go advisories that are **reachable** from this module's code, rather than every advisory against something in `go.mod`. It runs with `-mod=mod`, not vendor: it needs the module graph to decide what is reachable and cannot read one from a vendored tree, which makes this one of the two jobs here allowed network access. The toolchain comes from `go-version-file`, which is load-bearing - the only finding this check has ever produced was a standard library advisory, cleared by raising the floor in `go.mod` rather than by changing a dependency.

`images` builds a controller image locally, saves it to a tar, and scans it with trivy at `CRITICAL,HIGH`, `--ignore-unfixed`, `--scanners vuln`. The trivy version is a pin no real manager tracks, so it carries a `renovate:` annotation and Renovate moves it.

Both tools are downloaded and run directly rather than through their published actions. Each wrapper failed the job with its own step reported as "skipped" and no diagnostic of any kind, and the invocation either one wraps is two to four lines.

`vendor/` being committed reports advisories that are not this module's; [osv vendor overrides](../agent/choice-osv-vendor-overrides.md) explains what is done about that and what is left.
