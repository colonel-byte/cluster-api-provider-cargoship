// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/pkg/distro"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

// RecordLister reads the package records a registry holds.
//
// It exists so the resolution can be exercised without a registry to talk to. The one
// implementation outside tests is distro.ListMirrored, which SetupWithManager installs.
type RecordLister func(ctx context.Context, target string, opts distro.ListMirroredOptions) ([]distro.MirrorRecord, error)

// distroSource returns the package reference to converge to, and records it in status.
//
// A reference that names a digest is recorded as one. A reference that names a tag is used as
// given, and this is the one real limitation of the current version: **a tag that is republished
// underneath a running cluster is not picked up.** The desired-state fingerprint hashes the
// reference, so a rebuilt package behind the same tag produces the same fingerprint and no
// converge. Changing spec.distro.ociRef -- to a new tag, or to a digest -- is how a new package is
// adopted.
//
// Resolving a tag to a digest here would fix that, and the primitive is nearly there: coci.Resolve
// gives back a pinned reference, but it insists on a layer selector and a distro package carries
// many layers, so it returns ErrLayerRequired. What is missing is a cargoship-side "resolve this
// reference's root digest" with no selector. Until then, pinning in the reference is the
// recommended practice and the only way to be certain what a fleet converged to. See
// docs/agent/choice-distro-pinning.md.
func distroSource(
	ctx context.Context,
	cp *controlplanev1.CargoshipControlPlane,
	list RecordLister,
	caBundle []byte,
) (string, error) {
	// A name rather than a reference asks the registry what it holds. The schema allows exactly
	// one of the two, so this is the whole branch.
	if cp.Spec.Distro.Name != "" {
		return discoverDistro(ctx, cp, list, caBundle)
	}

	ref := strings.TrimSpace(cp.Spec.Distro.OCIRef)
	if ref == "" {
		return "", fmt.Errorf("spec.distro.ociRef is empty")
	}

	cp.Status.Distro.Ref = ref
	// Cleared rather than left stale, for the same reason the digest below is: a resource moved
	// from a discovered package to an explicit reference must not keep reporting what the
	// registry's record said.
	cp.Status.Distro.RecordDigest = ""
	if digest, ok := digestOf(ref); ok {
		cp.Status.Distro.Digest = digest
	} else {
		// Cleared rather than left stale: a reference that used to name a digest and now names a
		// tag must not keep reporting the old digest as what is installed.
		cp.Status.Distro.Digest = ""
	}

	return ref, nil
}

// discoverDistro resolves spec.distro.registry and spec.distro.name into the reference the
// package was published at, by reading the records cargoship leaves in the registry.
//
// The listing is scoped to the one package name, which is what keeps this to a single tag query
// against <registry>/cargoship -- a registry's catalogue is a wider permission than pushing to it
// and several registries do not serve one at all. See docs/agent/choice-record-discovery.md.
//
// Credentials come from the Docker configuration file the controller keeps on its cache volume,
// which spec.distro.pullSecretRef populates, so nothing is passed for them here: ListMirrored
// falls back to that file when it is given no username, which is the same path the package pull
// itself takes.
func discoverDistro(
	ctx context.Context,
	cp *controlplanev1.CargoshipControlPlane,
	list RecordLister,
	caBundle []byte,
) (string, error) {
	src := cp.Spec.Distro
	target := strings.TrimSuffix(strings.TrimSpace(src.Registry), "/")

	opts := distro.ListMirroredOptions{
		Name:     src.Name,
		CABundle: caBundle,
	}
	// Set through the promoted field rather than by naming cargoship's embedded
	// types.RemoteOptions, which would make zarf a direct dependency of this module for one bool.
	opts.InsecureSkipTLSVerify = src.InsecureSkipTLSVerify

	records, err := list(ctx, target, opts)
	if err != nil {
		return "", fmt.Errorf("unable to read the packages recorded in %s: %w", target, err)
	}

	record, err := pickRecord(records, src.Name, src.PackageVersion)
	if err != nil {
		return "", fmt.Errorf("%s: %w", target, err)
	}

	// The reference a publish writes, which coci.ReferenceFromMetadataWithOptions builds the same
	// way: the target, the package's name, and the version as a tag. The record's version is the
	// tag that was actually published rather than the package's own, so a package published under
	// --tag is still pullable from it.
	ref := fmt.Sprintf("oci://%s/%s:%s", target, record.Name, record.Version)

	cp.Status.Distro.Ref = ref
	cp.Status.Distro.RecordDigest = record.Digest
	// Empty, and deliberately: the record's digest is computed over the package's contents, while
	// the reference above resolves to a multi-architecture index whose digest is a different
	// number. Reporting one as the other would make status.distro.digest a lie about something an
	// operator would reasonably paste into spec.distro.ociRef. See choice-distro-pinning.md.
	cp.Status.Distro.Digest = ""

	return ref, nil
}

// pickRecord chooses the package record to converge to.
//
// Only a package record names something to pull. A mirror record is the other half of an air gap
// -- the images and files a node will ask for, pushed under their own references -- so a registry
// holding nothing but those has no package to converge from, and that is the one failure here
// worth its own message: the fix is a `cargoship publish --record`, not a different version.
//
// Whether the chosen version's contents were mirrored into the same registry is deliberately not
// reported. It is knowable from the records read here and it is not acted on, so saying it would
// need a status field or a condition of its own; a converge that cannot pull an image fails with
// the image's name in it, which is a better answer than a warning written minutes earlier.
//
// A version is always named -- the schema requires it alongside a name -- so nothing here picks
// one. Records arrive newest first, so the first match is the current record for that version in
// the unlikely event a registry serves two.
func pickRecord(records []distro.MirrorRecord, name, version string) (distro.MirrorRecord, error) {
	var packages, mirrors []distro.MirrorRecord
	for _, record := range records {
		if record.Name != name {
			continue
		}
		switch record.Kind {
		case distro.RecordKindPackage:
			packages = append(packages, record)
		case distro.RecordKindMirror:
			mirrors = append(mirrors, record)
		}
	}

	if len(packages) == 0 {
		if len(mirrors) > 0 {
			return distro.MirrorRecord{}, fmt.Errorf(
				"%s is recorded as mirrored into this registry but not published into it, so there is no package to pull: publish it with `cargoship publish --record`",
				name)
		}
		return distro.MirrorRecord{}, fmt.Errorf("no record of a package named %s", name)
	}

	// Checked rather than assumed, even though the schema refuses a name without a version: a
	// custom resource created against an earlier CRD is still in etcd afterwards, and the
	// fallback an empty version invites -- take the newest -- is the behaviour this deliberately
	// does not have. See AdoptRepublishedPackageAnnotation.
	if version == "" {
		return distro.MirrorRecord{}, fmt.Errorf(
			"no packageVersion for %s; recorded versions are %s",
			name, strings.Join(recordedVersions(packages), ", "))
	}

	for _, record := range packages {
		if record.Version == version {
			return record, nil
		}
	}
	return distro.MirrorRecord{}, fmt.Errorf(
		"no record of %s %s; recorded versions are %s",
		name, version, strings.Join(recordedVersions(packages), ", "))
}

// recordedVersions lists the versions on offer, sorted so that the message an operator reads
// does not depend on the order the registry returned tags in.
func recordedVersions(records []distro.MirrorRecord) []string {
	versions := make([]string, 0, len(records))
	for _, record := range records {
		versions = append(versions, record.Version)
	}
	sort.Strings(versions)
	return versions
}

// digestOf returns the digest a reference already names, if it names one.
func digestOf(ref string) (string, bool) {
	at := strings.LastIndex(ref, "@")
	if at < 0 || at == len(ref)-1 {
		return "", false
	}
	return ref[at+1:], true
}
