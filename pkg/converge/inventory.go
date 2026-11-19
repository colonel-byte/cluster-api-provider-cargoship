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
	"errors"
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

		// The private address is what the nodes use to talk to each other and what
		// the firewall phase adds to the cluster trust set (k8s-nodes ipset).
		// Always populate it: if an InternalIP is present use it, otherwise fall back
		// to the connection address.
		if private := addressOfType(m, clusterv1.MachineInternalIP); private != "" {
			host.PrivateAddress = private
		} else {
			host.PrivateAddress = address
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

// ErrClusterConfig marks every error DecodeClusterConfig returns.
//
// A caller needs to tell this apart from the rest of inventory building, because the two want
// opposite answers: "no machine has an address yet" comes right on its own and is worth asking
// again about shortly, while a malformed cluster config is a mistake in the resource that no
// number of passes will fix, and reporting it as an incomplete inventory sends an operator to
// look at their machines.
var ErrClusterConfig = errors.New("spec.clusterConfig is not a valid cargoship cluster config")

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
		return cluster.Config{}, fmt.Errorf("%w: %w", ErrClusterConfig, err)
	}
	if err := validateClusterConfig(out); err != nil {
		return cluster.Config{}, fmt.Errorf("%w: %w", ErrClusterConfig, err)
	}
	return out, nil
}

// validateClusterConfig runs cargoship's own validators over a decoded config.
//
// It exists because the API server cannot: spec.clusterConfig is an opaque object with
// x-kubernetes-preserve-unknown-fields, and the CEL type resolver skips subtrees it has no schema
// for, so a validation rule naming self.clusterConfig.profiles does not merely fail to fire -- it
// fails to compile and the API server rejects the custom resource definition. A guard for this
// field has to be Go, and the only choice is where it runs.
//
// Here is the earliest point it can. Everything below checks something cargoship checks too, but
// checks it late: a firewall port is validated in cargoship's eleventh phase, which is four
// phases after it has opened an SSH connection to every host in the fleet, and a profile's
// concurrency is parsed by each phase as it sizes its batches, so a typo in one surfaces after
// earlier phases have already changed hosts. Both are plain mistakes in a hand-written document
// and neither needs a single host to be reachable to find.
//
// Nothing here is a second implementation of those checks. Each one calls the cargoship function
// that will run later anyway, so this cannot come to disagree with what an apply enforces.
func validateClusterConfig(cfg cluster.Config) error {
	if err := cluster.ValidateRegistries(cfg.Registries); err != nil {
		return err
	}

	// Validated against the folded profiles rather than as written, which is what a host actually
	// gets: a profile inheriting a bad concurrency from another is just as broken as one spelling
	// it out. The fold is discarded -- cargoship does it again when it loads the cluster -- so
	// this reads the config and changes nothing about what the converge is given.
	resolved, err := cluster.ResolveProfiles(cfg.Profiles)
	if err != nil {
		return err
	}
	for _, name := range sortedKeys(resolved) {
		profile := resolved[name]
		where := fmt.Sprintf("profiles.%s", name)
		if err := validateHostConfig(where+".host", profile.Host); err != nil {
			return err
		}
		// Against one host, which is only a stand-in for the batch size: every value this
		// rejects -- a negative count, a percentage over 100, anything not a number -- is
		// rejected whatever the fleet size, and no fleet size makes a valid one invalid.
		if _, err := cluster.ParseConcurrency(profile.Concurrency, 1); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	}

	return nil
}

// validateHostConfig checks the firewall rules, exposed ports and firewalld policies of one
// profile's host overrides. A profile is the only place they can come from here: the hosts of this
// inventory are built from Machines, so spec.clusterConfig carries no host of its own.
func validateHostConfig(where string, host cluster.HostConfig) error {
	if err := host.Firewall.Validate(); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	for i, port := range host.Ports {
		if err := validatePortSpec(fmt.Sprintf("%s.ports[%d]", where, i), port.Protocol, port.Port); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(host.Policy) {
		for i, port := range host.Policy[name].Ports {
			at := fmt.Sprintf("%s.policy.%s.ports[%d]", where, name, i)
			if err := validatePortSpec(at, port.Protocol, port.Port); err != nil {
				return err
			}
		}
	}
	return nil
}

// validatePortSpec checks one port and protocol pair through an equivalent firewall rule.
//
// cargoship's port parser is unexported and FirewallRule.Validate is the only exported thing that
// calls it, so a rule is built to get at it. The alternative is a second port parser in this
// repository, which would be the one thing this file is trying not to be: a copy of a check that
// can come to disagree with the one that matters.
func validatePortSpec(where, protocol, port string) error {
	if protocol == "" {
		// Every firewall backend renders a port with no protocol as tcp, so an empty one is
		// valid here and must not be handed to a rule, which requires it.
		protocol = "tcp"
	}
	rule := cluster.FirewallRule{
		Action:    "allow",
		Direction: "in",
		Protocol:  protocol,
		Port:      port,
	}
	if err := rule.Validate(); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	return nil
}

// sortedKeys returns a map's keys in order, so a config with two mistakes in it reports the same
// one every time rather than whichever the map iterated to first.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
//
// The topology label is read first, and is not an alternative spelling of the second: for a
// deployment a ClusterClass generated, the two hold different things. Cluster API sets
// topology.cluster.x-k8s.io/deployment-name to the *class* name -- "gpu" -- while the object's own
// name, which cluster.x-k8s.io/deployment-name would carry, is generated per cluster and looks
// like "fleet-one-gpu-x7k2p". It also stops its webhook adding that second label to a managed
// deployment at all, calling it "redundant for managed MachineDeployments given that we already
// have topology.cluster.x-k8s.io/deployment-name"
// (exp/topology/desiredstate/desired_state.go, cluster-api v1.14.4), and sets the topology label
// in .spec.template.labels so it reaches each Machine.
//
// So reading only the second label means every pool declared through a class selects no profile
// at all, and takes the defaults without saying so -- which is why this is a fallback rather than
// the answer. A hand-wired MachineDeployment carries only the second and is unaffected.
func profile(m *clusterv1.Machine) string {
	if class, ok := m.Labels[clusterv1.ClusterTopologyMachineDeploymentNameLabel]; ok && class != "" {
		return class
	}
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
