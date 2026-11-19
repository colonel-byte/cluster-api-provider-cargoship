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
			err := ApplyRegistryAuth(&cfg, tt.payload)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for i, want := range tt.want {
				if got := cfg.Registries[i].Authentication; got != want {
					t.Errorf("registry %s: got %+v, want %+v", cfg.Registries[i].Name, got, want)
				}
			}
		})
	}
}
