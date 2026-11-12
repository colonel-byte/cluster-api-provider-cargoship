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
	"fmt"
	"strings"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

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
func distroSource(cp *controlplanev1.CargoshipControlPlane) (string, error) {
	ref := strings.TrimSpace(cp.Spec.Distro.OCIRef)
	if ref == "" {
		return "", fmt.Errorf("spec.distro.ociRef is empty")
	}

	cp.Status.Distro.Ref = ref
	if digest, ok := digestOf(ref); ok {
		cp.Status.Distro.Digest = digest
	} else {
		// Cleared rather than left stale: a reference that used to name a digest and now names a
		// tag must not keep reporting the old digest as what is installed.
		cp.Status.Distro.Digest = ""
	}

	return ref, nil
}

// digestOf returns the digest a reference already names, if it names one.
func digestOf(ref string) (string, bool) {
	at := strings.LastIndex(ref, "@")
	if at < 0 || at == len(ref)-1 {
		return "", false
	}
	return ref[at+1:], true
}
