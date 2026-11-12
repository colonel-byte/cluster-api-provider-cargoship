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

package cloudinit

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	t.Parallel()

	got, err := Render(Config{
		User:           "rocky",
		AuthorizedKeys: []string{"ssh-ed25519 AAAA controller", "ssh-ed25519 BBBB operator"},
		Files: []File{
			{
				Path:        "/etc/pki/ca-trust/source/anchors/internal.crt",
				Owner:       "root:root",
				Permissions: "0644",
				Content:     "-----BEGIN CERTIFICATE-----\n",
			},
		},
		RunCmd: []string{"update-ca-trust"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := string(got)

	// cloud-init dispatches on this line. Without it the document is ignored silently, which is
	// the single most expensive way for this to be wrong.
	if !strings.HasPrefix(out, "#cloud-config\n") {
		t.Errorf("output does not start with the #cloud-config header:\n%s", out)
	}
	for _, want := range []string{
		"name: rocky",
		"ssh_authorized_keys:",
		"ssh-ed25519 AAAA controller",
		"ssh-ed25519 BBBB operator",
		"write_files:",
		"/etc/pki/ca-trust/source/anchors/internal.crt",
		"permissions: \"0644\"",
		"runcmd:",
		"update-ca-trust",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// TestRenderIsDeterministic matters because the rendered bytes are what the bootstrap secret
// holds. If they reordered between renders, every reconcile would look like a change and rewrite
// the secret -- which on most infrastructure providers is user-data a running machine has already
// consumed.
func TestRenderIsDeterministic(t *testing.T) {
	t.Parallel()

	c := Config{
		User:           "rocky",
		AuthorizedKeys: []string{"ssh-ed25519 AAAA controller"},
		Files: []File{
			{
				Path:    "/etc/one",
				Content: "1",
			},
			{
				Path:    "/etc/two",
				Content: "2",
			},
		},
		RunCmd: []string{"a", "b"},
	}

	first, err := Render(c)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for range 20 {
		again, err := Render(c)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("Render is not deterministic:\n%s\n---\n%s", first, again)
		}
	}
}

func TestRenderRefusesAMachineNothingCouldLogInto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       Config
		wantFrag string
	}{
		{
			name:     "no user",
			in:       Config{AuthorizedKeys: []string{"ssh-ed25519 AAAA"}},
			wantFrag: "no user",
		},
		{
			// Rendered anyway, this produces a machine the infrastructure provider reports
			// ready and the control plane then requeues against forever.
			name:     "no keys",
			in:       Config{User: "rocky"},
			wantFrag: "no authorized keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Render(tt.in); err == nil {
				t.Fatalf("Render succeeded, want an error mentioning %q", tt.wantFrag)
			} else if !strings.Contains(err.Error(), tt.wantFrag) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantFrag)
			}
		})
	}
}
