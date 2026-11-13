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
	"encoding/json"
	"testing"
)

func baseState() DesiredState {
	return DesiredState{
		Hosts: []DesiredHost{
			{
				Address: "10.0.0.11",
				Role:    RoleController,
			},
			{
				Address: "10.0.0.21",
				Role:    RoleWorker,
				Profile: "gpu",
			},
		},
		Endpoint:      "api.fleet.example",
		DistroRef:     "oci://example/k3s@sha256:aaaa",
		ClusterConfig: json.RawMessage(`{"values":{"cni":"cilium"}}`),
	}
}

func fingerprint(t *testing.T, d DesiredState) string {
	t.Helper()

	got, err := d.Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	return got
}

// TestFingerprintIsStable is the property the whole cost control rests on. If the fingerprint
// moves on its own, every resync re-runs a multi-minute SSH walk over the fleet.
func TestFingerprintIsStable(t *testing.T) {
	t.Parallel()

	if a, b := fingerprint(t, baseState()), fingerprint(t, baseState()); a != b {
		t.Errorf("the same state fingerprinted differently:\n %s\n %s", a, b)
	}
}

// TestFingerprintIgnoresHostOrder covers the realistic way it would move on its own: the API
// server is under no obligation to return machines in the same order twice.
func TestFingerprintIgnoresHostOrder(t *testing.T) {
	t.Parallel()

	reversed := baseState()
	reversed.Hosts = []DesiredHost{reversed.Hosts[1], reversed.Hosts[0]}

	if a, b := fingerprint(t, baseState()), fingerprint(t, reversed); a != b {
		t.Errorf("reordering the hosts changed the fingerprint:\n %s\n %s", a, b)
	}
}

func TestFingerprintChanges(t *testing.T) {
	t.Parallel()

	base := fingerprint(t, baseState())

	tests := []struct {
		name   string
		mutate func(*DesiredState)
	}{
		{
			name: "a host is added",
			mutate: func(d *DesiredState) {
				d.Hosts = append(d.Hosts, DesiredHost{
					Address: "10.0.0.22",
					Role:    RoleWorker,
				})
			},
		},
		{
			name: "a host is removed",
			mutate: func(d *DesiredState) {
				d.Hosts = d.Hosts[:1]
			},
		},
		{
			name: "a host changes role",
			mutate: func(d *DesiredState) {
				d.Hosts[1].Role = RoleController
			},
		},
		{
			name: "a host changes profile",
			mutate: func(d *DesiredState) {
				d.Hosts[1].Profile = "cpu"
			},
		},
		{
			name: "the endpoint changes",
			mutate: func(d *DesiredState) {
				d.Endpoint = "other.example"
			},
		},
		{
			// A republished tag resolves to a new digest, and that has to count as a change --
			// otherwise a fleet never picks up a rebuilt package.
			name: "the package reference changes",
			mutate: func(d *DesiredState) {
				d.DistroRef = "oci://example/k3s@sha256:bbbb"
			},
		},
		{
			name: "the cluster config changes",
			mutate: func(d *DesiredState) {
				d.ClusterConfig = json.RawMessage(`{"values":{"cni":"calico"}}`)
			},
		},
		{
			name: "an apply option changes",
			mutate: func(d *DesiredState) {
				d.Apply = json.RawMessage(`{"modifyFirewall":true}`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := baseState()
			tt.mutate(&d)
			if got := fingerprint(t, d); got == base {
				t.Errorf("the fingerprint did not change, so this would not trigger a converge")
			}
		})
	}
}
