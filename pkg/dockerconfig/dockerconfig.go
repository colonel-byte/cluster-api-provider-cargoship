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

// Package dockerconfig gives a distro pull a credential store to read.
//
// Credentials reach a pull through exactly one path, and it is not a parameter. Cargoship's
// distro.Load builds the oras modifier list itself inside pullOCI, so oci.WithCredentialFunc is
// not reachable from a caller; what is left is the fallback oras takes when no credential function
// is set, which is the Docker credential store at $DOCKER_CONFIG/config.json, read each time a
// remote is built. So the controller writes that file.
//
// This is the one secret the provider puts on a filesystem -- the SSH key deliberately never
// reaches one, see pkg/sshkey -- and the reasoning for the difference is in
// docs/agent/choice-registry-credentials.md.
package dockerconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// EnvVar is the variable oras reads the credential store's directory from.
const EnvVar = "DOCKER_CONFIG"

// fileName is what oras looks for inside that directory.
const fileName = "config.json"

// mu serializes the read-modify-write below. One file is shared by every CargoshipControlPlane,
// because $DOCKER_CONFIG is process-wide while converges for different clusters run concurrently.
var mu sync.Mutex

// config is the subset of a Docker config file that matters here. Each entry is kept as raw JSON
// so that whatever the secret held -- `auth`, a username and password, an identity token -- is
// written back unchanged rather than through a struct that would drop the fields it lacks.
type config struct {
	Auths map[string]json.RawMessage `json:"auths"`
}

// Use creates dir and points $DOCKER_CONFIG at it. It must be called before Merge.
func Use(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("unable to create the docker config directory %s: %w", dir, err)
	}
	if err := os.Setenv(EnvVar, dir); err != nil {
		return fmt.Errorf("unable to set %s: %w", EnvVar, err)
	}
	return nil
}

// Merge adds the registries from a kubernetes.io/dockerconfigjson payload to the config file,
// keeping the entries for registries the payload does not name.
//
// Merging by registry is what makes one shared file safe: two clusters pulling from two registries
// each find their own entry, and neither reconcile has to wait for the other.
func Merge(payload []byte) error {
	dir := os.Getenv(EnvVar)
	if dir == "" {
		return fmt.Errorf("%s is not set: Use has not been called", EnvVar)
	}

	incoming, err := parse(payload)
	if err != nil {
		return fmt.Errorf("unable to read the pull secret: %w", err)
	}
	if len(incoming.Auths) == 0 {
		return errors.New("the pull secret names no registries")
	}

	mu.Lock()
	defer mu.Unlock()

	path := filepath.Join(dir, fileName)
	current := &config{}
	switch b, err := os.ReadFile(path); {
	case err == nil:
		if current, err = parse(b); err != nil {
			return fmt.Errorf("unable to read %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("unable to read %s: %w", path, err)
	}

	if current.Auths == nil {
		current.Auths = map[string]json.RawMessage{}
	}
	for registry, entry := range incoming.Auths {
		current.Auths[registry] = entry
	}
	// shortcut: an entry stays once written, outliving the CargoshipControlPlane that wrote it.
	// Upgrade this to pruning the registries no resource names any more if a credential ever has
	// to be revocable from here rather than from the registry.

	b, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("unable to serialize %s: %w", path, err)
	}

	// Written beside the file and renamed over it, because a pull in flight reads it: a
	// half-written file is a failed converge, where an atomic replacement is at worst a pull that
	// used the previous credentials.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("unable to write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("unable to replace %s: %w", path, err)
	}
	return nil
}

func parse(b []byte) (*config, error) {
	c := &config{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}
