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

// Package converge turns Cluster API objects into a cargoship inventory and runs a converge
// against it.
//
// It is the only package in this repository that imports cargoship, and it is shared by both
// controllers. Everything above it deals in Machines and custom resources; everything below it is
// cargoship's own phase model.
package converge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	rigssh "github.com/k0sproject/rig/v2/protocol/ssh"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// Roles, as cargoship spells them.
const (
	RoleController = "controller"
	RoleWorker     = "worker"
)

// SSHParams is how the controller reaches the machines.
type SSHParams struct {
	// User is the login to connect as.
	User string
	// Port is the SSH port. Zero means rig's default of 22.
	Port int
	// PrivateKey is the PEM-encoded private key, read from a secret.
	//
	// It is handed to rig as an in-memory auth method and never written to a file. rig's
	// ssh.Config.KeyPath is a path, and materializing a key to satisfy it would put cluster-wide
	// credentials on a container filesystem for no gain.
	PrivateKey []byte
	// AddressTypes is the order in which a machine's addresses are tried, most preferred first.
	// Empty means InternalIP, then ExternalIP, then Hostname.
	AddressTypes []clusterv1.MachineAddressType
}

// defaultAddressTypes prefers the private address.
//
// A cargoship fleet is normally on a network the management cluster shares, and an air-gapped one
// has no external address at all. A fleet whose machines are only reachable by public address sets
// the order explicitly.
var defaultAddressTypes = []clusterv1.MachineAddressType{
	clusterv1.MachineInternalIP,
	clusterv1.MachineExternalIP,
	clusterv1.MachineInternalDNS,
	clusterv1.MachineExternalDNS,
	clusterv1.MachineHostName,
}

// Input is everything needed to build one inventory.
type Input struct {
	// ClusterName names the cluster, and becomes the inventory's metadata.name.
	ClusterName string
	// Endpoint is the host clients reach the API through, and becomes spec.config.loadbalancer.
	Endpoint string
	// ClusterConfig is spec.config from the custom resource, already decoded. Its LoadBalancer is
	// overwritten by Endpoint.
	ClusterConfig cluster.Config
	// SSH is how to reach the machines.
	SSH SSHParams
	// Machines are every Machine belonging to the cluster, control plane and workers alike. Ones
	// with no usable address are skipped rather than refused.
	Machines []*clusterv1.Machine
}

// Host pairs a built cargoship host with the Machine it came from, so a caller can map a converge
// result back onto the objects it has to write status to.
type Host struct {
	// Host is the entry in the inventory.
	Host *cluster.Host
	// Machine is the Machine it was built from.
	Machine *clusterv1.Machine
	// Address is the address the inventory addresses it by, which is the identity cargoship's own
	// results use.
	Address string
	// Role is controller or worker.
	Role string
}

// Inventory is a cargoship cluster configuration and the machine mapping behind it.
type Inventory struct {
	// Cluster is what a phase manager takes as its Config.
	Cluster cluster.Cluster
	// Hosts is the per-machine mapping, in the same order as Cluster.Spec.Hosts.
	Hosts []Host
	// Skipped names the machines left out because they have no usable address yet. It is not an
	// error: it is the normal state of a machine the infrastructure provider has not finished
	// with, and the reason this controller needs no timers.
	Skipped []string
}

// BuildInventory assembles a cargoship inventory from the machines that are reachable now.
//
// Machines with no usable address are left out and named in Skipped. That is the whole of this
// provider's answer to the timing problem: the desired host set is recomputed from scratch on
// every reconcile, so a machine that takes eleven minutes to get an address joins the converge
// that follows it getting one, and no part of the system holds a deadline for it.
//
// It returns an error only for things that cannot come right on their own: no key, no user, no
// machine with an address at all.
func BuildInventory(in Input) (*Inventory, error) {
	if in.SSH.User == "" {
		return nil, fmt.Errorf("ssh user is empty")
	}
	if len(in.SSH.PrivateKey) == 0 {
		return nil, fmt.Errorf("ssh private key is empty")
	}

	authMethods, err := rigssh.ParseSSHPrivateKey(in.SSH.PrivateKey, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to use the ssh private key: %w", err)
	}

	order := in.SSH.AddressTypes
	if len(order) == 0 {
		order = defaultAddressTypes
	}

	// Sorted so the inventory, and therefore the desired-state hash, does not change because the
	// API server returned the same machines in a different order.
	machines := append([]*clusterv1.Machine(nil), in.Machines...)
	sort.Slice(machines, func(i, j int) bool { return machines[i].Name < machines[j].Name })

	inv := &Inventory{}
	inv.Cluster.APIVersion = "colonel-byte.dev/v1alpha1"
	inv.Cluster.Kind = "Cluster"
	inv.Cluster.Metadata.Name = in.ClusterName
	inv.Cluster.Spec.Config = in.ClusterConfig
	inv.Cluster.Spec.Config.LoadBalancer = in.Endpoint

	for _, m := range machines {
		// A machine being deleted is left out, which is the whole point of the pre-terminate
		// hook: Cluster API holds the instance while this converge rewrites the survivors'
		// /etc/hosts and firewall rules, and it can only rewrite them without the departing
		// host if the departing host is absent from the inventory. Not recorded in Skipped --
		// that is "has no address yet", a state that comes right on its own, and this one does
		// not.
		if !m.DeletionTimestamp.IsZero() {
			continue
		}

		address := pickAddress(m, order)
		if address == "" {
			inv.Skipped = append(inv.Skipped, m.Name)
			continue
		}

		sshConfig := &rigssh.Config{
			Address:     address,
			User:        in.SSH.User,
			Port:        in.SSH.Port,
			AuthMethods: authMethods,
			// There is no ~/.ssh/config in this pod, and no system ssh_config worth reading.
			// Left on, rig would look for both on every connection and silently inherit
			// whatever a base image happened to ship.
			IgnoreSSHConfig: true,
		}

		host := &cluster.Host{
			Role:    role(m),
			Profile: profile(m),
		}
		host.ConnectionConfig.SSH = sshConfig

		// The private address is what the nodes use to talk to each other, which is not
		// necessarily the address this controller reached them on. Setting it only when they
		// differ leaves cargoship to discover it in the ordinary case.
		if private := addressOfType(m, clusterv1.MachineInternalIP); private != "" && private != address {
			host.PrivateAddress = private
		}

		inv.Cluster.Spec.Hosts = append(inv.Cluster.Spec.Hosts, host)
		inv.Hosts = append(inv.Hosts, Host{
			Host:    host,
			Machine: m,
			Address: address,
			Role:    host.Role,
		})
	}

	if len(inv.Cluster.Spec.Hosts) == 0 {
		return nil, fmt.Errorf("no machine has a usable address yet (%d skipped)", len(inv.Skipped))
	}
	if !hasController(inv.Hosts) {
		return nil, fmt.Errorf("no control-plane machine has a usable address yet (%d skipped)", len(inv.Skipped))
	}

	return inv, nil
}

// DecodeClusterConfig reads spec.clusterConfig off a custom resource.
//
// The field is an opaque object in the custom resource and a typed struct here, so this is where a
// malformed one is caught. Unknown keys are rejected rather than ignored: the config is hand
// written, and a misspelled key that is silently dropped is a setting an operator believes is in
// force. A nil or empty value is a valid empty config -- a cluster with no registries, profiles or
// value overrides is perfectly ordinary.
func DecodeClusterConfig(raw []byte) (cluster.Config, error) {
	var out cluster.Config
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}

	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return cluster.Config{}, fmt.Errorf("spec.clusterConfig is not a valid cargoship cluster config: %w", err)
	}
	return out, nil
}

// pickAddress returns the first address of the first type the order lists.
func pickAddress(m *clusterv1.Machine, order []clusterv1.MachineAddressType) string {
	for _, t := range order {
		if a := addressOfType(m, t); a != "" {
			return a
		}
	}
	return ""
}

// addressOfType returns the machine's first address of the given type, or "".
func addressOfType(m *clusterv1.Machine, t clusterv1.MachineAddressType) string {
	for _, a := range m.Status.Addresses {
		if a.Type == t && a.Address != "" {
			return a.Address
		}
	}
	return ""
}

// role reads the machine's cargoship role off the control-plane label Cluster API sets.
func role(m *clusterv1.Machine) string {
	if _, ok := m.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		return RoleController
	}
	return RoleWorker
}

// profile is the cargoship profile the host selects, taken from the MachineDeployment that owns
// it.
//
// This is the join that makes a MachineDeployment mean something to cargoship: a deployment named
// "gpu" selects spec.clusterConfig.profiles.gpu, and gets that profile's node labels, taints,
// firewall rules and concurrency. A profile the config does not define is not an error -- the host
// just takes the defaults -- so a deployment needs a profile entry only when it should differ.
// Control-plane machines take no profile; they are not stamped from a deployment.
func profile(m *clusterv1.Machine) string {
	return m.Labels[clusterv1.MachineDeploymentNameLabel]
}

// hasController reports whether any built host is a controller.
//
// A converge with only workers cannot do anything useful: every phase that joins a node needs a
// leader to get a token from, and the leader is a controller.
func hasController(hosts []Host) bool {
	for _, h := range hosts {
		if h.Role == RoleController {
			return true
		}
	}
	return false
}
