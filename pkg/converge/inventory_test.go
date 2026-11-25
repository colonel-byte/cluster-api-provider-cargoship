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
	if cp.Host.Profile != ProfileControlPlane {
		t.Errorf("control-plane host profile = %q, want %q", cp.Host.Profile, ProfileControlPlane)
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
		// Keyed by machine identity, not address: a DHCP lease reused by a replacement machine
		// must not collide in known_hosts with its predecessor's key.
		if got := h.Host.ConnectionConfig.SSH.SSHConfigOptions["HostKeyAlias"]; got != h.Machine.Name {
			t.Errorf("%s host key alias = %v, want the machine name", h.Machine.Name, got)
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
			name:               "internal is preferred by default",
			addrs:              []clusterv1.MachineAddress{external("203.0.113.7"), internal("10.0.0.11")},
			wantAddress:        "10.0.0.11",
			wantPrivateAddress: "10.0.0.11",
		},
		{
			name:               "an explicit order can prefer external",
			addrs:              []clusterv1.MachineAddress{external("203.0.113.7"), internal("10.0.0.11")},
			order:              []clusterv1.MachineAddressType{clusterv1.MachineExternalIP},
			wantAddress:        "203.0.113.7",
			wantPrivateAddress: "10.0.0.11",
		},
		{
			name:               "falls through to a hostname",
			addrs:              []clusterv1.MachineAddress{{Type: clusterv1.MachineHostName, Address: "cp-0.example"}},
			wantAddress:        "cp-0.example",
			wantPrivateAddress: "cp-0.example",
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
			// The auth is not decoration: cargoship refuses a registry entry that configures
			// nothing, on the grounds that a name on its own is a mistyped document rather than
			// an intentional no-op.
			name: "a registry and values decode",
			raw:  `{"registries":[{"name":"registry.example","auth":{"user":"u","pass":"p"}}],"values":{"cni":"cilium"}}`,
		},
		{
			name: "a profile with a firewall rule, ports and a concurrency decodes",
			raw: `{"profiles":{"gpu":{"concurrency":"25%","host":{"ports":[{"port":"8080","protocol":"tcp"}],` +
				`"firewall":{"rules":[{"action":"allow","direction":"in","protocol":"tcp","port":"30000-32767"}]}}}}}`,
		},
		{
			// New in cargoship v0.43.1. The decoder refuses unknown keys, so this case is what
			// notices a vendored cargoship too old to know the field: it would fail with
			// "unknown field" rather than decode.
			name: "tlsSan decodes",
			raw:  `{"tlsSan":["vip.example.com","203.0.113.10"]}`,
		},
		{
			// New in cargoship v0.44.0, and the same canary as the case above: the decoder
			// refuses unknown keys, so a vendored cargoship too old to know the field fails
			// with "unknown field" rather than decode.
			name: "trustedSources decodes, as a CIDR block and as an inclusive range",
			raw:  `{"trustedSources":["10.0.0.0/16","10.1.2.2-10.1.2.150"]}`,
		},
		{
			// Caught here rather than in cargoship's twenty-sixth phase, which is after SSH
			// has connected to the fleet and earlier phases have already changed hosts.
			name:     "a trustedSources entry that is neither a CIDR block nor a range is refused",
			raw:      `{"trustedSources":["10.0.0.0"]}`,
			wantErr:  true,
			wantFrag: "trustedSources: invalid CIDR",
		},
		{
			name:     "a profile naming a concurrency that is not a count or a percentage is refused",
			raw:      `{"profiles":{"gpu":{"concurrency":"twenty-five%"}}}`,
			wantErr:  true,
			wantFrag: "profiles.gpu: invalid concurrency percentage",
		},
		{
			name:     "a percentage over 100 is refused",
			raw:      `{"profiles":{"gpu":{"concurrency":"150%"}}}`,
			wantErr:  true,
			wantFrag: "must be between 0 and 100",
		},
		{
			// Inherited rather than written here, which is the whole reason the profiles are
			// folded before they are checked.
			name:     "a concurrency inherited from another profile is refused too",
			raw:      `{"profiles":{"base":{"concurrency":"nope"},"gpu":{"inherit":"base"}}}`,
			wantErr:  true,
			wantFrag: "invalid concurrency",
		},
		{
			name:     "a profile inheriting a profile that does not exist is refused",
			raw:      `{"profiles":{"gpu":{"inherit":"missing"}}}`,
			wantErr:  true,
			wantFrag: "missing",
		},
		{
			name:     "a firewall rule port out of range is refused",
			raw:      `{"profiles":{"gpu":{"host":{"firewall":{"rules":[{"action":"allow","direction":"in","protocol":"tcp","port":"99999"}]}}}}}`,
			wantErr:  true,
			wantFrag: "profiles.gpu.host:",
		},
		{
			name:     "an exposed port out of range is refused",
			raw:      `{"profiles":{"gpu":{"host":{"ports":[{"port":"70000","protocol":"tcp"}]}}}}`,
			wantErr:  true,
			wantFrag: "profiles.gpu.host.ports[0]:",
		},
		{
			// Every firewall backend renders a port with no protocol as tcp, so this is a valid
			// document and the rule the port is checked through must not refuse it.
			name: "an exposed port with no protocol decodes",
			raw:  `{"profiles":{"gpu":{"host":{"ports":[{"port":"8080"}]}}}}`,
		},
		{
			name:     "a firewalld policy port out of range is refused",
			raw:      `{"profiles":{"gpu":{"host":{"policy":{"inbound":{"target":"ACCEPT","ports":[{"port":"0","protocol":"tcp"}]}}}}}}`,
			wantErr:  true,
			wantFrag: "profiles.gpu.host.policy.inbound.ports[0]:",
		},
		{
			name:     "a registry entry that configures nothing is refused",
			raw:      `{"registries":[{"name":"registry.example"}]}`,
			wantErr:  true,
			wantFrag: "registry.example",
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

func TestBuildInventoryLeavesOutMachinesBeingDeleted(t *testing.T) {
	t.Parallel()

	departing := machine("fleet-cp-1", true, "", internal("10.0.0.12"))
	now := metav1.Now()
	departing.DeletionTimestamp = &now
	departing.Finalizers = []string{"test"}

	inv, err := BuildInventory(Input{
		ClusterName: "fleet",
		Endpoint:    "10.0.0.11",
		SSH:         SSHParams{User: "rocky", PrivateKey: testKey(t)},
		Machines: []*clusterv1.Machine{
			machine("fleet-cp-0", true, "", internal("10.0.0.11")),
			departing,
		},
	})
	if err != nil {
		t.Fatalf("BuildInventory: %v", err)
	}

	// This is what the pre-terminate hook window is for: the converge that runs while Cluster API
	// holds the instance has to rewrite the survivors' /etc/hosts and firewall rules from a host
	// list the departing machine is not in.
	if len(inv.Hosts) != 1 || inv.Hosts[0].Address != "10.0.0.11" {
		t.Fatalf("built %d hosts %v, want only the surviving machine", len(inv.Hosts), inv.Hosts)
	}
	// Skipped means "no address yet", which comes right on its own. A machine being deleted does
	// not, and reporting it there would read as a fleet still waiting on its infrastructure.
	if len(inv.Skipped) != 0 {
		t.Errorf("Skipped = %v, want empty", inv.Skipped)
	}
}

// TestProfile covers the label the profile is read from, which decides whether a worker pool gets
// its node labels, taints, firewall rules and concurrency or silently takes the defaults.
//
// Two labels carry two different things. For a hand-wired MachineDeployment the object's own name
// is the profile. For one a ClusterClass generated, the object's name is generated per cluster and
// the class name - the thing an operator wrote - is in the topology label, which Cluster API also
// propagates to each Machine.
func TestProfile(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name: "a hand-wired deployment, named after the profile",
			labels: map[string]string{
				clusterv1.MachineDeploymentNameLabel: "gpu",
			},
			want: "gpu",
		},
		{
			name: "a deployment a class generated, where only the topology label is the class name",
			labels: map[string]string{
				clusterv1.ClusterTopologyMachineDeploymentNameLabel: "gpu",
			},
			want: "gpu",
		},
		{
			name: "both, which is the class name against a generated object name",
			labels: map[string]string{
				clusterv1.ClusterTopologyMachineDeploymentNameLabel: "gpu",
				clusterv1.MachineDeploymentNameLabel:                "fleet-one-gpu-x7k2p",
			},
			want: "gpu",
		},
		{
			name: "an empty topology label falls through rather than winning",
			labels: map[string]string{
				clusterv1.ClusterTopologyMachineDeploymentNameLabel: "",
				clusterv1.MachineDeploymentNameLabel:                "gpu",
			},
			want: "gpu",
		},
		{
			name:   "a control-plane machine, stamped from no deployment",
			labels: nil,
			want:   "",
		},
		{
			name: "a control-plane machine with the control-plane label",
			labels: map[string]string{
				clusterv1.MachineControlPlaneLabel: "",
			},
			want: ProfileControlPlane,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "fleet-worker",
					Labels: tt.labels,
				},
			}

			if got := Profile(m); got != tt.want {
				t.Errorf("Profile() = %q, want %q", got, tt.want)
			}
		})
	}
}
