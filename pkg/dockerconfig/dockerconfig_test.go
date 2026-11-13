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

package dockerconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeRejectsWhatCannotBeUsed(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "not json",
			payload: "zarf-docker-registry:5000",
			want:    "unable to read the pull secret",
		},
		{
			name:    "a dockerconfigjson with no registries",
			payload: `{"auths":{}}`,
			want:    "names no registries",
		},
		{
			name:    "an empty payload",
			payload: `{}`,
			want:    "names no registries",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useTempDir(t)

			err := Merge([]byte(tc.payload))
			if err == nil {
				t.Fatalf("Merge(%q) returned no error, want one containing %q", tc.payload, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Merge(%q) = %q, want it to contain %q", tc.payload, err, tc.want)
			}
		})
	}
}

// The reason the file is merged rather than written: two CargoshipControlPlanes pulling from two
// registries share one $DOCKER_CONFIG, and the second reconcile must not take the first's
// credentials away.
func TestMergeKeepsTheRegistriesItDoesNotName(t *testing.T) {
	dir := useTempDir(t)

	zarf := `{"auths":{"zarf-docker-registry.zarf.svc.cluster.local:5000":{"auth":"emFyZjpvbmU="}}}`
	ghcr := `{"auths":{"ghcr.io":{"auth":"Z2g6dHdv"}}}`

	if err := Merge([]byte(zarf)); err != nil {
		t.Fatalf("Merge(zarf) = %v, want no error", err)
	}
	if err := Merge([]byte(ghcr)); err != nil {
		t.Fatalf("Merge(ghcr) = %v, want no error", err)
	}

	auths := readAuths(t, filepath.Join(dir, fileName))
	for _, registry := range []string{"zarf-docker-registry.zarf.svc.cluster.local:5000", "ghcr.io"} {
		if _, ok := auths[registry]; !ok {
			t.Fatalf("the config file holds %v, want an entry for %q", keys(auths), registry)
		}
	}
}

func TestMergeReplacesTheRegistriesItDoesName(t *testing.T) {
	dir := useTempDir(t)

	if err := Merge([]byte(`{"auths":{"ghcr.io":{"auth":"b2xkOm9uZQ=="}}}`)); err != nil {
		t.Fatalf("Merge(old) = %v, want no error", err)
	}
	if err := Merge([]byte(`{"auths":{"ghcr.io":{"auth":"bmV3OnR3bw=="}}}`)); err != nil {
		t.Fatalf("Merge(new) = %v, want no error", err)
	}

	auths := readAuths(t, filepath.Join(dir, fileName))
	if got, want := string(auths["ghcr.io"]), `{"auth":"bmV3OnR3bw=="}`; got != want {
		t.Fatalf("the entry for ghcr.io is %s, want %s", got, want)
	}
}

func TestMergeWithoutUse(t *testing.T) {
	t.Setenv(EnvVar, "")

	if err := Merge([]byte(`{"auths":{"ghcr.io":{"auth":"Z2g6dHdv"}}}`)); err == nil {
		t.Fatal("Merge returned no error with DOCKER_CONFIG unset, want one")
	}
}

// useTempDir points DOCKER_CONFIG at a directory for one test. t.Setenv restores it afterwards,
// which matters because Use sets a process-wide variable.
func useTempDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "docker")
	t.Setenv(EnvVar, "")
	if err := Use(dir); err != nil {
		t.Fatalf("Use(%q) = %v, want no error", dir, err)
	}
	return dir
}

func readAuths(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("unable to read %s: %v", path, err)
	}
	c, err := parse(b)
	if err != nil {
		t.Fatalf("unable to parse %s: %v", path, err)
	}
	return c.Auths
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
