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

// Package cloudinit renders the bootstrap data a cargoship-managed machine needs.
//
// What it renders is deliberately almost nothing. Nothing here installs Kubernetes, writes an
// engine configuration, or joins a cluster -- all of that is a cargoship phase, run over SSH from
// the management cluster, where it is reported on and can be retried. The bootstrap data exists
// for one reason: an infrastructure provider will not create a machine until the Machine has a
// bootstrap data secret, and the machine has to accept an SSH connection before cargoship can do
// anything at all.
//
// So the output is an authorized key, sshd, and whatever the operator said must be true before
// the first connection. Everything a bootstrap script would normally carry is something that
// would run once, unreported, with no retry, on a host cargoship is about to take charge of.
package cloudinit

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

// Config is what goes into one machine's bootstrap data.
type Config struct {
	// User is the account whose authorized_keys the keys are appended to. It must already exist
	// in the image.
	User string
	// AuthorizedKeys are the public keys to append, the controller's own first.
	AuthorizedKeys []string
	// Files are written before RunCmd runs.
	Files []File
	// RunCmd are commands run on first boot.
	RunCmd []string
}

// File is one file to write.
type File struct {
	// Path is the absolute path to write to.
	Path string
	// Owner is the "user:group" to write it as.
	Owner string
	// Permissions is the file mode, as a string.
	Permissions string
	// Content is written literally.
	Content string
}

// cloudConfig is the shape cloud-init reads. It is a private type with explicit json tags rather
// than a map, so that the rendered output is deterministic -- a map would reorder keys between
// renders and make every bootstrap secret look changed.
type cloudConfig struct {
	Users      []cloudUser `json:"users,omitempty"`
	WriteFiles []cloudFile `json:"write_files,omitempty"`
	RunCmd     []string    `json:"runcmd,omitempty"`
}

type cloudUser struct {
	Name              string   `json:"name"`
	SSHAuthorizedKeys []string `json:"ssh_authorized_keys,omitempty"`
}

type cloudFile struct {
	Path        string `json:"path"`
	Owner       string `json:"owner,omitempty"`
	Permissions string `json:"permissions,omitempty"`
	Content     string `json:"content,omitempty"`
}

// Render returns the bootstrap data for one machine.
//
// It refuses a config with no user or no key, rather than rendering data that produces a machine
// nothing can log into. That failure would otherwise surface much later and much less clearly: the
// infrastructure provider would create the machine, report it ready, and the control plane would
// sit requeueing on an SSH connection that could never succeed.
func Render(c Config) ([]byte, error) {
	if c.User == "" {
		return nil, fmt.Errorf("no user to authorize keys for")
	}
	if len(c.AuthorizedKeys) == 0 {
		return nil, fmt.Errorf("no authorized keys, so nothing could ever connect to this machine")
	}

	cfg := cloudConfig{
		Users: []cloudUser{
			{
				Name: c.User,
				// cloud-init appends to the user's authorized_keys rather than replacing it,
				// so a key the image already carries survives.
				SSHAuthorizedKeys: c.AuthorizedKeys,
			},
		},
		RunCmd: c.RunCmd,
	}

	for _, f := range c.Files {
		// A conversion rather than a field-by-field copy: the two types hold the same fields and
		// differ only in their json tags, which is what makes the conversion legal. Keep them in
		// step -- adding a field to File and not to cloudFile stops this compiling, which is the
		// behaviour wanted.
		cfg.WriteFiles = append(cfg.WriteFiles, cloudFile(f))
	}

	body, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to render the bootstrap data: %w", err)
	}

	// The header is not optional and not cosmetic: cloud-init dispatches on it, and without it
	// the whole document is ignored in silence.
	var out strings.Builder
	out.WriteString("#cloud-config\n")
	out.Write(body)
	return []byte(out.String()), nil
}
