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

package converge

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

func TestApplyRegistryAuth(t *testing.T) {
	// "svc-user:svc-password" and "docker-user:docker-pass", base64.
	payload := []byte(`{
		"auths": {
			"registry.example.com": {"username": "svc-user", "password": "svc-password"},
			"https://index.docker.io/v1/": {"auth": "ZG9ja2VyLXVzZXI6ZG9ja2VyLXBhc3M="},
			"token.example.com": {"identitytoken": "tok-123"}
		}
	}`)

	tests := []struct {
		name       string
		registries []cluster.Registries
		payload    []byte
		want       []cluster.RegistryAuth
		wantErr    bool
	}{
		{
			name: "username and password are copied by host",
			registries: []cluster.Registries{
				{Name: "registry.example.com"},
			},
			payload: payload,
			want: []cluster.RegistryAuth{
				{Username: "svc-user", Password: "svc-password"},
			},
		},
		{
			name: "the docker hub auths key matches docker.io and the auth field is decoded",
			registries: []cluster.Registries{
				{Name: "docker.io"},
			},
			payload: payload,
			want: []cluster.RegistryAuth{
				{Username: "docker-user", Password: "docker-pass"},
			},
		},
		{
			name: "an identitytoken becomes a token",
			registries: []cluster.Registries{
				{Name: "token.example.com"},
			},
			payload: payload,
			want: []cluster.RegistryAuth{
				{Token: "tok-123"},
			},
		},
		{
			name: "inline auth wins over the secret",
			registries: []cluster.Registries{
				{
					Name:           "registry.example.com",
					Authentication: cluster.RegistryAuth{Username: "inline", Password: "kept"},
				},
			},
			payload: payload,
			want: []cluster.RegistryAuth{
				{Username: "inline", Password: "kept"},
			},
		},
		{
			name: "an unmatched registry is left alone when another matches",
			registries: []cluster.Registries{
				{Name: "registry.example.com"},
				{Name: "quay.io"},
			},
			payload: payload,
			want: []cluster.RegistryAuth{
				{Username: "svc-user", Password: "svc-password"},
				{},
			},
		},
		{
			name: "a secret naming only the mirror host covers a registry proxying through it",
			registries: []cluster.Registries{
				{
					Name:  "docker.io",
					Proxy: &cluster.RegistryProxy{URL: "https://mirror.example.com/artifactory/api/docker/docker-remote"},
				},
			},
			payload: []byte(`{"auths": {"mirror.example.com": {"username": "svc-user", "password": "svc-password"}}}`),
			want: []cluster.RegistryAuth{
				{Username: "svc-user", Password: "svc-password"},
			},
		},
		{
			name: "the mirror and the registry proxying through it end up with identical credentials",
			registries: []cluster.Registries{
				{Name: "mirror.example.com"},
				{
					Name:  "docker.io",
					Proxy: &cluster.RegistryProxy{URL: "https://mirror.example.com/artifactory/api/docker/docker-remote"},
				},
			},
			payload: []byte(`{"auths": {"mirror.example.com": {"username": "svc-user", "password": "svc-password"}}}`),
			want: []cluster.RegistryAuth{
				{Username: "svc-user", Password: "svc-password"},
				{Username: "svc-user", Password: "svc-password"},
			},
		},
		{
			name: "an entry named for the registry wins over the mirror fallback",
			registries: []cluster.Registries{
				{
					Name:  "docker.io",
					Proxy: &cluster.RegistryProxy{URL: "https://mirror.example.com/artifactory/api/docker/docker-remote"},
				},
			},
			payload: []byte(`{"auths": {
				"mirror.example.com": {"username": "mirror-user", "password": "mirror-pass"},
				"docker.io": {"username": "hub-user", "password": "hub-pass"}
			}}`),
			want: []cluster.RegistryAuth{
				{Username: "hub-user", Password: "hub-pass"},
			},
		},
		{
			name: "a secret matching nothing is an error",
			registries: []cluster.Registries{
				{Name: "quay.io"},
			},
			payload: payload,
			wantErr: true,
		},
		{
			name: "a payload with no auths is an error",
			registries: []cluster.Registries{
				{Name: "quay.io"},
			},
			payload: []byte(`{}`),
			wantErr: true,
		},
		{
			name: "an entry with no usable credential is an error",
			registries: []cluster.Registries{
				{Name: "empty.example.com"},
			},
			payload: []byte(`{"auths": {"empty.example.com": {}}}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := cluster.Config{Registries: tt.registries}
			hash, err := ApplyRegistryAuth(&cfg, tt.payload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hash == "" {
				t.Error("expected a non-empty hash")
			}
			for i, want := range tt.want {
				if got := cfg.Registries[i].Authentication; got != want {
					t.Errorf("registry %s: got %+v, want %+v", cfg.Registries[i].Name, got, want)
				}
			}
		})
	}
}

// TestApplyRegistryAuthMirrorPassesValidation pins the failure that motivated the mirror
// fallback: a mirror and a registry proxying through it both configure the mirror's host, and
// cargoship's ValidateRegistries rejects the pair when only one of them got the credentials.
func TestApplyRegistryAuthMirrorPassesValidation(t *testing.T) {
	cfg := cluster.Config{Registries: []cluster.Registries{
		{Name: "mirror.example.com"},
		{
			Name:  "docker.io",
			Proxy: &cluster.RegistryProxy{URL: "https://mirror.example.com/artifactory/api/docker/docker-remote"},
		},
	}}
	payload := []byte(`{"auths": {"mirror.example.com": {"username": "svc-user", "password": "svc-password"}}}`)

	if _, err := ApplyRegistryAuth(&cfg, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cluster.ValidateRegistries(cfg.Registries); err != nil {
		t.Errorf("ValidateRegistries rejected the pair after auth was applied: %v", err)
	}
}

// TestApplyRegistryAuthHash pins what makes the hash move: the credentials that landed on a
// registry, and nothing else. It is what lets a rotated secret be a fingerprint event while an
// irrelevant entry added to the same secret is not.
func TestApplyRegistryAuthHash(t *testing.T) {
	registries := func() []cluster.Registries {
		return []cluster.Registries{
			{Name: "registry.example.com"},
		}
	}
	apply := func(t *testing.T, payload string) string {
		t.Helper()
		cfg := cluster.Config{Registries: registries()}
		hash, err := ApplyRegistryAuth(&cfg, []byte(payload))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return hash
	}

	base := `{"auths": {"registry.example.com": {"username": "u", "password": "p1"}}}`

	first, second := apply(t, base), apply(t, base)
	if first != second {
		t.Error("the same payload must hash the same")
	}
	rotated := `{"auths": {"registry.example.com": {"username": "u", "password": "p2"}}}`
	if apply(t, base) == apply(t, rotated) {
		t.Error("a rotated password must change the hash")
	}
	widened := `{"auths": {"registry.example.com": {"username": "u", "password": "p1"}, "unrelated.example.com": {"username": "x", "password": "y"}}}`
	if apply(t, base) != apply(t, widened) {
		t.Error("an entry for a registry the cluster does not name must not change the hash")
	}
}
