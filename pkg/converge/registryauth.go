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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

// dockerConfig is the subset of a kubernetes.io/dockerconfigjson payload that carries
// credentials. Unlike pkg/dockerconfig, which keeps entries as raw JSON because it writes them
// back to a file, this needs the fields themselves: they are copied into cluster.Config in
// memory and never serialized.
type dockerConfig struct {
	Auths map[string]dockerAuth `json:"auths"`
}

type dockerAuth struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Auth          string `json:"auth"`
	IdentityToken string `json:"identitytoken"`
}

// ApplyRegistryAuth fills the credentials of cfg.Registries from a
// kubernetes.io/dockerconfigjson payload, in memory, before the config reaches an apply.
//
// An entry that already has credentials in the cluster config is left alone: inline auth wins,
// so a config that worked keeps working when a secret is added beside it. A registry the payload
// does not name is left alone too, because one secret routinely covers only part of a fleet's
// registries. But a payload that matches nothing is an error rather than a no-op: the operator
// wired a secret to this cluster, and silently authenticating nothing would surface as a 401
// deep inside cargoship against a registry they believed they had covered.
//
// It returns the hex SHA-256 of the credentials it applied, sorted by registry name, so a caller
// that wants a rotated secret to be an event can fold it into the fingerprint. Only what landed
// on a registry is hashed: an entry the secret carries for a registry this cluster does not name
// changes nothing on any host, and must not trigger a converge.
func ApplyRegistryAuth(cfg *cluster.Config, payload []byte) (string, error) {
	var dc dockerConfig
	if err := json.Unmarshal(payload, &dc); err != nil {
		return "", fmt.Errorf("unable to parse the registry auth payload: %w", err)
	}
	if len(dc.Auths) == 0 {
		return "", fmt.Errorf("the registry auth secret names no registries")
	}

	auths := make(map[string]dockerAuth, len(dc.Auths))
	for key, entry := range dc.Auths {
		auths[normalizeRegistryHost(key)] = entry
	}

	type applied struct {
		Name string               `json:"name"`
		Auth cluster.RegistryAuth `json:"auth"`
	}
	var landed []applied

	matched := 0
	for i := range cfg.Registries {
		reg := &cfg.Registries[i]
		entry, ok := auths[normalizeRegistryHost(string(reg.Name))]
		if !ok {
			continue
		}
		matched++
		if reg.Authentication != (cluster.RegistryAuth{}) {
			continue
		}
		auth, err := entry.credentials()
		if err != nil {
			return "", fmt.Errorf("registry %s: %w", reg.Name, err)
		}
		reg.Authentication = auth
		landed = append(landed, applied{Name: string(reg.Name), Auth: auth})
	}
	if matched == 0 {
		return "", fmt.Errorf("the registry auth secret matches no registry in spec.clusterConfig.registries")
	}

	sort.Slice(landed, func(i, j int) bool { return landed[i].Name < landed[j].Name })
	b, err := json.Marshal(landed)
	if err != nil {
		return "", fmt.Errorf("unable to hash the applied registry credentials: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// credentials turns one auths entry into cargoship's RegistryAuth, preferring the explicit
// username and password over the packed auth field, the way docker-shaped tools read the file.
func (a dockerAuth) credentials() (cluster.RegistryAuth, error) {
	if a.IdentityToken != "" {
		return cluster.RegistryAuth{Token: a.IdentityToken}, nil
	}
	if a.Username != "" && a.Password != "" {
		return cluster.RegistryAuth{Username: a.Username, Password: a.Password}, nil
	}
	if a.Auth == "" {
		return cluster.RegistryAuth{}, fmt.Errorf("the auth entry has no username and password, auth, or identitytoken")
	}
	decoded, err := base64.StdEncoding.DecodeString(a.Auth)
	if err != nil {
		return cluster.RegistryAuth{}, fmt.Errorf("the auth field is not valid base64: %w", err)
	}
	user, pass, ok := strings.Cut(string(decoded), ":")
	if !ok || user == "" {
		return cluster.RegistryAuth{}, fmt.Errorf("the auth field does not decode to user:password")
	}
	return cluster.RegistryAuth{Username: user, Password: pass}, nil
}

// normalizeRegistryHost reduces an auths key or a registry name to a bare host so the two can be
// compared: real dockerconfigjson files key Docker Hub as "https://index.docker.io/v1/" while a
// cluster config names it "docker.io".
//
// shortcut: exact host match only -- a proxy URL's host and a port-qualified mismatch are not
// considered; revisit when a fleet needs credentials matched against clusterConfig proxy mirrors.
func normalizeRegistryHost(key string) string {
	host := key
	if _, rest, ok := strings.Cut(host, "://"); ok {
		host = rest
	}
	host, _, _ = strings.Cut(host, "/")
	if host == "index.docker.io" || host == "registry-1.docker.io" {
		return "docker.io"
	}
	return host
}
