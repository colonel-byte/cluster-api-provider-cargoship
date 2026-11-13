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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// testKey returns a throwaway PEM-encoded ed25519 private key.
//
// Generated rather than carried as a fixture: a checked-in private key is a thing somebody
// eventually finds in a scanner report and has to prove is harmless.
func testKey(t *testing.T) []byte {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshalling the key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// machine builds a Machine with the given name, role and addresses.
func machine(name string, controlPlane bool, deployment string, addrs ...clusterv1.MachineAddress) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
	}
	if controlPlane {
		m.Labels[clusterv1.MachineControlPlaneLabel] = ""
	}
	if deployment != "" {
		m.Labels[clusterv1.MachineDeploymentNameLabel] = deployment
	}
	m.Status.Addresses = addrs
	return m
}

func internal(ip string) clusterv1.MachineAddress {
	return clusterv1.MachineAddress{
		Type:    clusterv1.MachineInternalIP,
		Address: ip,
	}
}

func external(ip string) clusterv1.MachineAddress {
	return clusterv1.MachineAddress{
		Type:    clusterv1.MachineExternalIP,
		Address: ip,
	}
}

func TestBuildInventory(t *testing.T) {
	t.Parallel()
	key := testKey(t)

	in := Input{
		ClusterName: "fleet",
		Endpoint:    "api.fleet.example",
		SSH: SSHParams{
			User:       "rocky",
			Port:       22,
			PrivateKey: key,
		},
		Machines: []*clusterv1.Machine{
			// Deliberately out of name order, to prove the sort.
			machine("fleet-md-0-abc", false, "gpu", internal("10.0.0.21")),
			machine("fleet-cp-0", true, "", internal("10.0.0.11")),
			// No address yet: the infrastructure provider has not finished. Must be skipped,
			// not refused -- this is the normal state of a machine mid-provision, and the
			// whole reason this provider needs no timers.
			machine("fleet-md-0-pending", false, "gpu"),
		},
	}

	inv, err := BuildInventory(in)
	if err != nil {
		t.Fatalf("BuildInventory: %v", err)
	}

	if got := len(inv.Cluster.Spec.Hosts); got != 2 {
		t.Fatalf("built %d hosts, want 2", got)
	}
	if len(inv.Skipped) != 1 || inv.Skipped[0] != "fleet-md-0-pending" {
		t.Errorf("Skipped = %v, want [fleet-md-0-pending]", inv.Skipped)
	}
	if inv.Cluster.Metadata.Name != "fleet" {
		t.Errorf("metadata.name = %q, want %q", inv.Cluster.Metadata.Name, "fleet")
	}
	// The contract's controlPlaneEndpoint wins over anything spec.clusterConfig set, because
	// Cluster API copies it onto the Cluster and two different answers would be two different
	// clusters.
	if inv.Cluster.Spec.Config.LoadBalancer != "api.fleet.example" {
		t.Errorf("loadbalancer = %q, want %q", inv.Cluster.Spec.Config.LoadBalancer, "api.fleet.example")
	}

	// Sorted by machine name, so fleet-cp-0 comes first.
	cp := inv.Hosts[0]
	if cp.Machine.Name != "fleet-cp-0" {
		t.Fatalf("first host is %q, want fleet-cp-0 (hosts are not sorted by machine name)", cp.Machine.Name)
	}
	if cp.Role != RoleController {
		t.Errorf("control-plane machine built role %q, want %q", cp.Role, RoleController)
	}
	if cp.Host.Profile != "" {
		t.Errorf("control-plane host profile = %q, want empty", cp.Host.Profile)
	}
	if cp.Address != "10.0.0.11" {
		t.Errorf("control-plane address = %q, want 10.0.0.11", cp.Address)
	}

	worker := inv.Hosts[1]
	if worker.Role != RoleWorker {
		t.Errorf("worker built role %q, want %q", worker.Role, RoleWorker)
	}
	// The MachineDeployment name is the cargoship profile. This is the join that makes a
	// deployment mean something to cargoship, and it is easy to break by reading the wrong label.
	if worker.Host.Profile != "gpu" {
		t.Errorf("worker profile = %q, want %q", worker.Host.Profile, "gpu")
	}

	for _, h := range inv.Hosts {
		if h.Host.ConnectionConfig.SSH == nil {
			t.Fatalf("%s has no ssh config", h.Machine.Name)
		}
		// The key must reach rig as an in-memory auth method, never as a path. A regression here
		// is invisible until a real connection is attempted.
		if len(h.Host.ConnectionConfig.SSH.AuthMethods) == 0 {
			t.Errorf("%s has no ssh auth methods, so the in-memory key was not wired up", h.Machine.Name)
		}
		if h.Host.ConnectionConfig.SSH.KeyPath != nil {
			t.Errorf("%s has a key path set; the key must never touch the filesystem", h.Machine.Name)
		}
		if !h.Host.ConnectionConfig.SSH.IgnoreSSHConfig {
			t.Errorf("%s reads ~/.ssh/config, which does not exist in a pod", h.Machine.Name)
		}
		if h.Host.ConnectionConfig.SSH.User != "rocky" {
			t.Errorf("%s connects as %q, want rocky", h.Machine.Name, h.Host.ConnectionConfig.SSH.User)
		}
	}
}

func TestBuildInventoryAddressSelection(t *testing.T) {
	t.Parallel()
	key := testKey(t)

	tests := []struct {
		name               string
		addrs              []clusterv1.MachineAddress
		order              []clusterv1.MachineAddressType
		wantAddress        string
		wantPrivateAddress string
	}{
		{
			name:        "internal is preferred by default",
			addrs:       []clusterv1.MachineAddress{external("203.0.113.7"), internal("10.0.0.11")},
			wantAddress: "10.0.0.11",
			// The connection address already is the private one, so there is nothing to
			// override and cargoship discovers it.
			wantPrivateAddress: "",
		},
		{
			name:               "an explicit order can prefer external",
			addrs:              []clusterv1.MachineAddress{external("203.0.113.7"), internal("10.0.0.11")},
			order:              []clusterv1.MachineAddressType{clusterv1.MachineExternalIP},
			wantAddress:        "203.0.113.7",
			wantPrivateAddress: "10.0.0.11",
		},
		{
			name:        "falls through to a hostname",
			addrs:       []clusterv1.MachineAddress{{Type: clusterv1.MachineHostName, Address: "cp-0.example"}},
			wantAddress: "cp-0.example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inv, err := BuildInventory(Input{
				ClusterName: "fleet",
				SSH: SSHParams{
					User:         "rocky",
					PrivateKey:   key,
					AddressTypes: tt.order,
				},
				Machines: []*clusterv1.Machine{machine("cp-0", true, "", tt.addrs...)},
			})
			if err != nil {
				t.Fatalf("BuildInventory: %v", err)
			}

			if got := inv.Hosts[0].Address; got != tt.wantAddress {
				t.Errorf("address = %q, want %q", got, tt.wantAddress)
			}
			if got := inv.Hosts[0].Host.PrivateAddress; got != tt.wantPrivateAddress {
				t.Errorf("privateAddress = %q, want %q", got, tt.wantPrivateAddress)
			}
		})
	}
}

func TestBuildInventoryErrors(t *testing.T) {
	t.Parallel()
	key := testKey(t)

	tests := []struct {
		name     string
		in       Input
		wantFrag string
	}{
		{
			name: "no user",
			in: Input{
				SSH:      SSHParams{PrivateKey: key},
				Machines: []*clusterv1.Machine{machine("cp-0", true, "", internal("10.0.0.11"))},
			},
			wantFrag: "ssh user is empty",
		},
		{
			name: "no key",
			in: Input{
				SSH:      SSHParams{User: "rocky"},
				Machines: []*clusterv1.Machine{machine("cp-0", true, "", internal("10.0.0.11"))},
			},
			wantFrag: "ssh private key is empty",
		},
		{
			name: "an unusable key",
			in: Input{
				SSH: SSHParams{
					User:       "rocky",
					PrivateKey: []byte("not a key"),
				},
				Machines: []*clusterv1.Machine{machine("cp-0", true, "", internal("10.0.0.11"))},
			},
			wantFrag: "unable to use the ssh private key",
		},
		{
			name: "nothing has an address yet",
			in: Input{
				SSH: SSHParams{
					User:       "rocky",
					PrivateKey: key,
				},
				Machines: []*clusterv1.Machine{machine("cp-0", true, "")},
			},
			wantFrag: "no machine has a usable address",
		},
		{
			// A fleet of workers and no reachable controller cannot converge: every join phase
			// needs a leader to take a token from. Reported distinctly, because "wait for the
			// control plane" and "wait for anything at all" are different situations.
			name: "workers but no controller",
			in: Input{
				SSH: SSHParams{
					User:       "rocky",
					PrivateKey: key,
				},
				Machines: []*clusterv1.Machine{
					machine("md-0", false, "gpu", internal("10.0.0.21")),
					machine("cp-0", true, ""),
				},
			},
			wantFrag: "no control-plane machine has a usable address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := BuildInventory(tt.in)
			if err == nil {
				t.Fatalf("BuildInventory succeeded, want an error mentioning %q", tt.wantFrag)
			}
			if !strings.Contains(err.Error(), tt.wantFrag) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantFrag)
			}
		})
	}
}

func TestDecodeClusterConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		wantErr  bool
		wantFrag string
		check    func(*testing.T, any)
	}{
		{
			name: "empty is a valid empty config",
			raw:  "",
		},
		{
			name: "null is a valid empty config",
			raw:  "null",
		},
		{
			name: "a registry and values decode",
			raw:  `{"registries":[{"name":"registry.example"}],"values":{"cni":"cilium"}}`,
		},
		{
			// A silently dropped key is a setting an operator believes is in force. This config
			// is hand written and never machine generated, so strictness costs nothing.
			name:     "an unknown key is refused",
			raw:      `{"registires":[{"name":"registry.example"}]}`,
			wantErr:  true,
			wantFrag: "not a valid cargoship cluster config",
		},
		{
			name:     "malformed json is refused",
			raw:      `{"values":`,
			wantErr:  true,
			wantFrag: "not a valid cargoship cluster config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeClusterConfig([]byte(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodeClusterConfig(%q) succeeded, want an error", tt.raw)
				}
				if !strings.Contains(err.Error(), tt.wantFrag) {
					t.Errorf("error = %q, want it to mention %q", err, tt.wantFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeClusterConfig(%q): %v", tt.raw, err)
			}
		})
	}
}
