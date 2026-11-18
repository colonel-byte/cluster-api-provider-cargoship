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
	"unicode/utf8"

	"sigs.k8s.io/yaml"
)

// FuzzRender checks that nothing an operator can put in a CargoshipConfig escapes the document it
// is rendered into. Every string here comes from a custom resource -- a user name, an authorized
// key, a file's path, owner, mode and content -- and the output is a YAML document cloud-init
// parses on a machine the controller cannot reach yet. A value that broke out of its field would
// produce a machine that boots, reports ready, and refuses every connection.
//
// So the invariant is that the rendered document parses and gives back exactly what went in, plus
// the `#cloud-config` header, which cloud-init dispatches on and ignores the document without.
func FuzzRender(f *testing.F) {
	f.Add("rocky", "ssh-ed25519 AAAA controller", "/etc/pki/ca-trust/source/anchors/internal.crt", "root:root", "0644", "-----BEGIN CERTIFICATE-----\n")
	f.Add("root", "ssh-rsa BBBB", "/etc/sysctl.d/99.conf", "", "", "")
	f.Add("a", "#cloud-config\nruncmd:\n- rm -rf /", "x", "y", "z", "runcmd: [whoami]")
	f.Add("- name: root", "key: value", "- path: /etc/shadow", ":", "{}", "...\n---\n")

	f.Fuzz(func(t *testing.T, user, key, path, owner, perms, content string) {
		// sigs.k8s.io/yaml marshals through JSON, which replaces a byte sequence that is not
		// valid UTF-8 with U+FFFD rather than failing. The round trip below cannot hold for an
		// input that was never representable, and a CargoshipConfig's strings arrive from the API
		// server, which rejects invalid UTF-8 before a reconcile ever sees it.
		//
		// U+0085 (NEL) is excluded for a separate reason: the YAML emitter writes it literally
		// inside single quotes and the parser then folds it to a space, so a value carrying one
		// is not round-trippable through YAML at all. U+2028 and U+2029 survive, and nothing else
		// found below is lossy.
		for _, s := range []string{user, key, path, owner, perms, content} {
			if !utf8.ValidString(s) {
				t.Skip("not valid UTF-8, so it cannot survive a JSON round trip")
			}
			if strings.ContainsRune(s, '\u0085') {
				t.Skip("YAML folds U+0085 to a space, so it cannot survive a YAML round trip")
			}
		}

		in := Config{
			User:           user,
			AuthorizedKeys: []string{key},
			Files: []File{
				{
					Path:        path,
					Owner:       owner,
					Permissions: perms,
					Content:     content,
				},
			},
			RunCmd: []string{content},
		}

		got, err := Render(in)
		if err != nil {
			// Refusing a config is a documented outcome; rendering a wrong one is not.
			return
		}

		body, ok := strings.CutPrefix(string(got), "#cloud-config\n")
		if !ok {
			t.Fatalf("rendered data does not start with the #cloud-config header:\n%s", got)
		}

		var out cloudConfig
		if err := yaml.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("rendered data is not valid YAML: %v\n%s", err, got)
		}

		if len(out.Users) != 1 {
			t.Fatalf("got %d users, want 1:\n%s", len(out.Users), got)
		}
		if out.Users[0].Name != user {
			t.Errorf("user = %q, want %q", out.Users[0].Name, user)
		}
		if len(out.Users[0].SSHAuthorizedKeys) != 1 || out.Users[0].SSHAuthorizedKeys[0] != key {
			t.Errorf("keys = %q, want [%q]", out.Users[0].SSHAuthorizedKeys, key)
		}
		if len(out.WriteFiles) != 1 {
			t.Fatalf("got %d files, want 1:\n%s", len(out.WriteFiles), got)
		}
		if want := (cloudFile{Path: path, Owner: owner, Permissions: perms, Content: content}); out.WriteFiles[0] != want {
			t.Errorf("file = %+v, want %+v", out.WriteFiles[0], want)
		}
		if len(out.RunCmd) != 1 || out.RunCmd[0] != content {
			t.Errorf("runcmd = %q, want [%q]", out.RunCmd, content)
		}
	})
}
