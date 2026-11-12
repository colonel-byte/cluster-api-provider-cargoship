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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// DesiredState is everything a converge's outcome depends on.
//
// It exists to be hashed. A reconcile that finds the hash unchanged, and every machine joined,
// skips the apply -- which matters because an apply is a multi-minute SSH walk and a controller
// resyncs for all sorts of reasons that have nothing to do with the fleet.
//
// This is a cost control and not a correctness gate, which is worth being clear about: cargoship's
// apply is a converge, so running it again is safe. Nothing is lost by clearing
// status.lastAppliedHash to force one, and that is the supported way to say "do it anyway".
type DesiredState struct {
	// Hosts is the host set, as the inventory addresses it.
	Hosts []DesiredHost `json:"hosts"`
	// ClusterConfig is spec.clusterConfig, as given.
	ClusterConfig json.RawMessage `json:"clusterConfig,omitempty"`
	// Endpoint is the control-plane endpoint.
	Endpoint string `json:"endpoint,omitempty"`
	// DistroRef is the package reference, pinned to a digest where the caller has one.
	//
	// A digest makes "the package changed" and "the fleet changed" the same kind of event. A tag
	// does not: a republished package behind the same tag hashes identically and so triggers no
	// converge. That is a known limit of the current version rather than a choice; see
	// docs/agent/choice-distro-pinning.md in the provider repository.
	DistroRef string `json:"distroRef,omitempty"`
	// Apply is the converge's own options, because they change what an apply does.
	Apply json.RawMessage `json:"apply,omitempty"`
}

// DesiredHost is one host's contribution to the fingerprint.
//
// It deliberately holds nothing that changes without the fleet changing. A machine's phase,
// conditions, node reference and resource version all move on their own, and folding any of them
// in would re-run an apply every time Cluster API wrote a status.
type DesiredHost struct {
	Address        string `json:"address"`
	Role           string `json:"role"`
	Profile        string `json:"profile,omitempty"`
	PrivateAddress string `json:"privateAddress,omitempty"`
}

// Fingerprint returns the hex SHA-256 of the desired state.
//
// Hosts are sorted by address first, so the hash does not depend on the order the API server
// returned machines in.
func (d DesiredState) Fingerprint() (string, error) {
	hosts := append([]DesiredHost(nil), d.Hosts...)
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Address < hosts[j].Address })
	d.Hosts = hosts

	// json.Marshal sorts map keys, so a config whose YAML key order changed hashes the same. The
	// raw fields are passed through as the API server stored them, which is already canonical
	// JSON.
	b, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("unable to fingerprint the desired state: %w", err)
	}

	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// DesiredHosts projects an inventory's hosts into the hashable form.
func (i *Inventory) DesiredHosts() []DesiredHost {
	out := make([]DesiredHost, 0, len(i.Hosts))
	for _, h := range i.Hosts {
		out = append(out, DesiredHost{
			Address:        h.Address,
			Role:           h.Role,
			Profile:        h.Host.Profile,
			PrivateAddress: h.Host.PrivateAddress,
		})
	}
	return out
}
