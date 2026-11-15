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
	"errors"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/distro"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

// records is the listing a registry holding two versions of one package, plus somebody else's,
// would return: newest first, as ListMirrored sorts it.
func records() []distro.MirrorRecord {
	return []distro.MirrorRecord{
		{
			Kind:    distro.RecordKindPackage,
			Name:    "k3s",
			Version: "1.36.1",
			Digest:  "sha256:newer",
		},
		{
			Kind:    distro.RecordKindMirror,
			Name:    "k3s",
			Version: "1.36.1",
			Images:  42,
		},
		{
			Kind:    distro.RecordKindPackage,
			Name:    "k3s",
			Version: "1.35.4",
			Digest:  "sha256:older",
		},
		{
			Kind:    distro.RecordKindPackage,
			Name:    "rancher-rke2-multi-cilium",
			Version: "1.36.1",
			Digest:  "sha256:other",
		},
	}
}

func TestPickRecord(t *testing.T) {
	tests := []struct {
		name       string
		records    []distro.MirrorRecord
		pkg        string
		version    string
		wantDigest string
		wantErr    string
	}{
		{
			name:       "the named version, not the newest one",
			records:    records(),
			pkg:        "k3s",
			version:    "1.35.4",
			wantDigest: "sha256:older",
		},
		{
			name:       "a package record, never the mirror record beside it",
			records:    records(),
			pkg:        "k3s",
			version:    "1.36.1",
			wantDigest: "sha256:newer",
		},
		{
			name:    "a version nothing recorded says what is on offer",
			records: records(),
			pkg:     "k3s",
			version: "1.37.0",
			wantErr: "no record of k3s 1.37.0; recorded versions are 1.35.4, 1.36.1",
		},
		{
			name:    "a package nothing recorded at all",
			records: records(),
			pkg:     "talos",
			version: "1.36.1",
			wantErr: "no record of a package named talos",
		},
		{
			name: "mirrored but never published, which is a different problem",
			records: []distro.MirrorRecord{
				{
					Kind:    distro.RecordKindMirror,
					Name:    "k3s",
					Version: "1.36.1",
				},
			},
			pkg:     "k3s",
			version: "1.36.1",
			wantErr: "mirrored into this registry but not published into it",
		},
		{
			name:    "no version is refused rather than resolved to the newest",
			records: records(),
			pkg:     "k3s",
			version: "",
			wantErr: "no packageVersion for k3s",
		},
		{
			name:    "an empty registry",
			records: nil,
			pkg:     "k3s",
			version: "1.36.1",
			wantErr: "no record of a package named k3s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickRecord(tt.records, tt.pkg, tt.version)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("pickRecord() = %+v, want an error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("pickRecord() error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("pickRecord() error = %v", err)
			}
			if got.Digest != tt.wantDigest {
				t.Errorf("pickRecord() digest = %q, want %q", got.Digest, tt.wantDigest)
			}
			if got.Kind != distro.RecordKindPackage {
				t.Errorf("pickRecord() kind = %q, want a package record", got.Kind)
			}
		})
	}
}

func TestDistroSource(t *testing.T) {
	list := func(_ context.Context, target string, opts distro.ListMirroredOptions) ([]distro.MirrorRecord, error) {
		if target != "zarf-docker-registry.zarf.svc.cluster.local:5000" {
			return nil, errors.New("unexpected target " + target)
		}
		// Scoped to the one package, which is what keeps this off the registry's catalogue.
		if opts.Name != "k3s" {
			return nil, errors.New("unscoped listing")
		}
		return records(), nil
	}

	t.Run("a discovered package becomes the reference a publish wrote", func(t *testing.T) {
		cp := &controlplanev1.CargoshipControlPlane{
			Spec: controlplanev1.CargoshipControlPlaneSpec{
				Distro: controlplanev1.DistroSource{
					Registry:       "zarf-docker-registry.zarf.svc.cluster.local:5000",
					Name:           "k3s",
					PackageVersion: "1.36.1",
				},
			},
		}

		source, err := distroSource(context.Background(), cp, list, nil)
		if err != nil {
			t.Fatalf("distroSource() error = %v", err)
		}

		want := "oci://zarf-docker-registry.zarf.svc.cluster.local:5000/k3s:1.36.1"
		if source != want {
			t.Errorf("distroSource() = %q, want %q", source, want)
		}
		if cp.Status.Distro.Ref != want {
			t.Errorf("status.distro.ref = %q, want %q", cp.Status.Distro.Ref, want)
		}
		if cp.Status.Distro.RecordDigest != "sha256:newer" {
			t.Errorf("status.distro.recordDigest = %q, want the record's digest", cp.Status.Distro.RecordDigest)
		}
		// The record's digest is the package's contents, not the index the reference resolves
		// to, so it must not be reported as the thing an operator could pin to.
		if cp.Status.Distro.Digest != "" {
			t.Errorf("status.distro.digest = %q, want it empty for a discovered package", cp.Status.Distro.Digest)
		}
	})

	t.Run("an explicit reference does not read the registry", func(t *testing.T) {
		cp := &controlplanev1.CargoshipControlPlane{
			Spec: controlplanev1.CargoshipControlPlaneSpec{
				Distro: controlplanev1.DistroSource{
					OCIRef: "oci://ghcr.io/example/k3s@sha256:abc",
				},
			},
		}
		refuse := func(context.Context, string, distro.ListMirroredOptions) ([]distro.MirrorRecord, error) {
			t.Fatal("the registry was read for a reference that names the package already")
			return nil, nil
		}

		source, err := distroSource(context.Background(), cp, refuse, nil)
		if err != nil {
			t.Fatalf("distroSource() error = %v", err)
		}
		if source != "oci://ghcr.io/example/k3s@sha256:abc" {
			t.Errorf("distroSource() = %q, want the reference as given", source)
		}
		if cp.Status.Distro.Digest != "sha256:abc" {
			t.Errorf("status.distro.digest = %q, want the digest the reference names", cp.Status.Distro.Digest)
		}
		if cp.Status.Distro.RecordDigest != "" {
			t.Errorf("status.distro.recordDigest = %q, want it empty", cp.Status.Distro.RecordDigest)
		}
	})

	t.Run("a registry that cannot be read names the registry", func(t *testing.T) {
		cp := &controlplanev1.CargoshipControlPlane{
			Spec: controlplanev1.CargoshipControlPlaneSpec{
				Distro: controlplanev1.DistroSource{
					Registry:       "zarf-docker-registry.zarf.svc.cluster.local:5000",
					Name:           "k3s",
					PackageVersion: "1.36.1",
				},
			},
		}
		broken := func(context.Context, string, distro.ListMirroredOptions) ([]distro.MirrorRecord, error) {
			return nil, errors.New("401 Unauthorized")
		}

		_, err := distroSource(context.Background(), cp, broken, nil)
		if err == nil {
			t.Fatal("distroSource() error = nil, want the read failure")
		}
		if !strings.Contains(err.Error(), "zarf-docker-registry.zarf.svc.cluster.local:5000") ||
			!strings.Contains(err.Error(), "401 Unauthorized") {
			t.Errorf("distroSource() error = %q, want it to name both the registry and the cause", err)
		}
	})
}

// TestAdoptRepublished covers the gate on following a republished package. Nothing about a
// registry is involved: what is being pinned down is that an operator has to ask.
func TestAdoptRepublished(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        string
	}{
		{
			name:        "unannotated, so a republished package changes nothing",
			annotations: nil,
			want:        "",
		},
		{
			name: "asked for",
			annotations: map[string]string{
				controlplanev1.AdoptRepublishedPackageAnnotation: "true",
			},
			want: "sha256:newer",
		},
		{
			name: "anything other than true is not consent",
			annotations: map[string]string{
				controlplanev1.AdoptRepublishedPackageAnnotation: "yes",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &controlplanev1.CargoshipControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
				Status: controlplanev1.CargoshipControlPlaneStatus{
					Distro: controlplanev1.DistroStatus{
						RecordDigest: "sha256:newer",
					},
				},
			}

			if got := adoptRepublished(cp); got != tt.want {
				t.Errorf("adoptRepublished() = %q, want %q", got, tt.want)
			}
		})
	}
}
