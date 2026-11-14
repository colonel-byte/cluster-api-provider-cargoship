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

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/zarfstate"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/dockerconfig"
)

// zarfRegistry is the state a `zarf init` leaves behind: both credential pairs, and a managed
// certificate whose CA is in the cluster rather than in the state.
func zarfRegistry() zarfstate.Registry {
	return zarfstate.Registry{
		Address:      "127.0.0.1:31999",
		Port:         31999,
		Mode:         zarfstate.ModeNodePort,
		PushUsername: "zarf-push",
		PushPassword: "push-secret",
		PullUsername: "zarf-pull",
		PullPassword: "pull-secret",
	}
}

func zarfControlPlane() *controlplanev1.CargoshipControlPlane {
	return &controlplanev1.CargoshipControlPlane{
		Spec: controlplanev1.CargoshipControlPlaneSpec{
			Distro: controlplanev1.DistroSource{
				FromZarfState:  true,
				Registry:       "zarf-docker-registry.zarf.svc.cluster.local:5000/mirror",
				Name:           "k3s",
				PackageVersion: "1.36.1",
			},
		},
	}
}

func TestStageFromZarfState(t *testing.T) {
	t.Run("the pull credentials reach the file the pull reads", func(t *testing.T) {
		dir := dockerConfigDir(t)
		r := &CargoshipControlPlaneReconciler{
			ZarfState: func(context.Context, zarfstate.Options) (zarfstate.Registry, []byte, error) {
				return zarfRegistry(), []byte("-----BEGIN CERTIFICATE-----\n"), nil
			},
		}
		cp := zarfControlPlane()

		caBundle, err := r.stageRegistryAccess(context.Background(), cp)
		if err != nil {
			t.Fatalf("stageRegistryAccess() error = %v", err)
		}
		if !strings.HasPrefix(string(caBundle), "-----BEGIN CERTIFICATE-----") {
			t.Errorf("stageRegistryAccess() ca = %q, want the CA the state carried", caBundle)
		}

		// Keyed by the host alone. The credential store looks an entry up by what the pull asks
		// for, so an entry written under the prefixed registry would never be found.
		auths := dockerAuths(t, dir)
		entry, ok := auths["zarf-docker-registry.zarf.svc.cluster.local:5000"]
		if !ok {
			t.Fatalf("the docker config holds %v, want an entry keyed by the registry host", keysOf(auths))
		}
		if !strings.Contains(string(entry), "zarf-pull") {
			t.Errorf("the entry is %s, want the pull credentials rather than the push pair", entry)
		}
	})

	t.Run("a state with only push credentials still works", func(t *testing.T) {
		dir := dockerConfigDir(t)
		registry := zarfRegistry()
		registry.PullUsername, registry.PullPassword = "", ""
		r := &CargoshipControlPlaneReconciler{
			ZarfState: func(context.Context, zarfstate.Options) (zarfstate.Registry, []byte, error) {
				return registry, nil, nil
			},
		}

		if _, err := r.stageRegistryAccess(context.Background(), zarfControlPlane()); err != nil {
			t.Fatalf("stageRegistryAccess() error = %v", err)
		}

		entry := dockerAuths(t, dir)["zarf-docker-registry.zarf.svc.cluster.local:5000"]
		if !strings.Contains(string(entry), "zarf-push") {
			t.Errorf("the entry is %s, want the push credentials as the fallback", entry)
		}
	})

	t.Run("an mTLS registry is reached with its CA and no client certificate", func(t *testing.T) {
		dir := dockerConfigDir(t)
		registry := zarfRegistry()
		registry.MTLSStrategy = "zarf-managed"
		r := &CargoshipControlPlaneReconciler{
			ZarfState: func(context.Context, zarfstate.Options) (zarfstate.Registry, []byte, error) {
				return registry, []byte("-----BEGIN CERTIFICATE-----\n"), nil
			},
		}
		cp := zarfControlPlane()

		// Zarf verifies a client certificate where one is offered and never requires it, so the
		// strategy is a log line rather than a refusal. See choice-registry-credentials.md.
		caBundle, err := r.stageRegistryAccess(context.Background(), cp)
		if err != nil {
			t.Fatalf("stageRegistryAccess() error = %v, want the mTLS strategy to be no obstacle", err)
		}
		if len(caBundle) == 0 {
			t.Error("stageRegistryAccess() returned no ca bundle, which is the whole requirement here")
		}
		if _, ok := dockerAuths(t, dir)["zarf-docker-registry.zarf.svc.cluster.local:5000"]; !ok {
			t.Error("the docker config holds no entry for the registry")
		}
	})

	t.Run("a cluster with no zarf state reports it on the resource", func(t *testing.T) {
		dockerConfigDir(t)
		r := &CargoshipControlPlaneReconciler{
			ZarfState: func(context.Context, zarfstate.Options) (zarfstate.Registry, []byte, error) {
				return zarfstate.Registry{}, nil, errors.New("secrets \"zarf-state\" not found")
			},
		}
		cp := zarfControlPlane()

		if _, err := r.stageRegistryAccess(context.Background(), cp); err == nil {
			t.Fatal("stageRegistryAccess() error = nil, want the read failure")
		}
		if reason := convergedReason(cp); reason != "ZarfStateUnreadable" {
			t.Errorf("the Converged condition's reason is %q, want ZarfStateUnreadable", reason)
		}
	})
}

func TestRegistryHost(t *testing.T) {
	tests := []struct {
		name     string
		registry string
		want     string
	}{
		{
			name:     "a host and a port",
			registry: "zarf-docker-registry.zarf.svc.cluster.local:5000",
			want:     "zarf-docker-registry.zarf.svc.cluster.local:5000",
		},
		{
			name:     "a repository prefix is not part of the key",
			registry: "ghcr.io/example/mirror",
			want:     "ghcr.io",
		},
		{
			name:     "surrounding space",
			registry: "  ghcr.io  ",
			want:     "ghcr.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := registryHost(tt.registry); got != tt.want {
				t.Errorf("registryHost(%q) = %q, want %q", tt.registry, got, tt.want)
			}
		})
	}
}

// dockerConfigDir points $DOCKER_CONFIG at a directory for one test, the way the manager points
// it at its cache volume. t.Setenv restores it afterwards, which matters because the variable is
// process-wide.
func dockerConfigDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "docker")
	t.Setenv(dockerconfig.EnvVar, "")
	if err := dockerconfig.Use(dir); err != nil {
		t.Fatalf("dockerconfig.Use(%q) = %v", dir, err)
	}
	return dir
}

func dockerAuths(t *testing.T, dir string) map[string]json.RawMessage {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("unable to read the docker config: %v", err)
	}
	var cfg struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unable to read the docker config: %v", err)
	}
	return cfg.Auths
}

func keysOf(auths map[string]json.RawMessage) []string {
	names := make([]string, 0, len(auths))
	for name := range auths {
		names = append(names, name)
	}
	return names
}

// convergedReason is the reason on the Converged condition, or "" when nothing set it.
func convergedReason(cp *controlplanev1.CargoshipControlPlane) string {
	for _, condition := range cp.Status.Conditions {
		if condition.Type == controlplanev1.ConvergedCondition {
			return condition.Reason
		}
	}
	return ""
}
