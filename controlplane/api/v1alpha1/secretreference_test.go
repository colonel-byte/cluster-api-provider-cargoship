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

package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestSecretReferenceKey(t *testing.T) {
	tests := []struct {
		name    string
		ref     SecretReference
		allowed []string
		want    types.NamespacedName
		wantErr bool
	}{
		{
			name: "no namespace is the owner's, which is every reference written before the field existed",
			ref: SecretReference{
				Name: "fleet-ssh",
			},
			want: types.NamespacedName{
				Namespace: "fleet",
				Name:      "fleet-ssh",
			},
		},
		{
			name: "the owner's own namespace needs no permission",
			ref: SecretReference{
				Name:      "fleet-ssh",
				Namespace: "fleet",
			},
			want: types.NamespacedName{
				Namespace: "fleet",
				Name:      "fleet-ssh",
			},
		},
		{
			name: "another namespace, permitted",
			ref: SecretReference{
				Name:      "fleet-ssh",
				Namespace: "shared-creds",
			},
			allowed: []string{"other", "shared-creds"},
			want: types.NamespacedName{
				Namespace: "shared-creds",
				Name:      "fleet-ssh",
			},
		},
		{
			name: "another namespace, not permitted",
			ref: SecretReference{
				Name:      "fleet-ssh",
				Namespace: "shared-creds",
			},
			allowed: []string{"other"},
			wantErr: true,
		},
		{
			name: "another namespace with nothing permitted, which is the default",
			ref: SecretReference{
				Name:      "fleet-ssh",
				Namespace: "shared-creds",
			},
			wantErr: true,
		},
		{
			name: "a star permits any namespace",
			ref: SecretReference{
				Name:      "fleet-ssh",
				Namespace: "shared-creds",
			},
			allowed: []string{"*"},
			want: types.NamespacedName{
				Namespace: "shared-creds",
				Name:      "fleet-ssh",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.ref.Key("fleet", tt.allowed)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Key returned %s, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Key: %v", err)
			}
			if got != tt.want {
				t.Errorf("Key = %s, want %s", got, tt.want)
			}
		})
	}
}
