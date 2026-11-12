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

// Package sshkey reads the controller's SSH key out of a secret.
//
// The key never reaches a filesystem. The bytes are read from the API server, handed to rig as an
// in-memory auth method, and that is the whole lifecycle -- see pkg/converge.SSHParams. This
// package exists because both controllers need the same two things from the same secret: the
// bootstrap provider needs the public half to authorize, and the control-plane provider needs the
// private half to connect.
package sshkey

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
)

// PrivateKeyField is the key within the secret holding the private key. It is the field name of a
// kubernetes.io/ssh-auth secret, so `kubectl create secret generic --type` produces one directly.
const PrivateKeyField = corev1.SSHAuthPrivateKey

// PrivateKey returns the PEM-encoded private key from a secret.
func PrivateKey(secret *corev1.Secret) ([]byte, error) {
	key, ok := secret.Data[PrivateKeyField]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s has no %q field", secret.Namespace, secret.Name, PrivateKeyField)
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("secret %s/%s has an empty %q field", secret.Namespace, secret.Name, PrivateKeyField)
	}
	return key, nil
}

// AuthorizedKey derives the authorized_keys line from the private key in a secret.
//
// Deriving it rather than carrying the public half separately removes a way for the two to get out
// of step. A mismatched pair produces a fleet of machines that boot correctly, report ready, and
// refuse every connection the controller makes -- a failure that looks like a network problem for
// as long as it takes somebody to compare the two keys by hand.
//
// An encrypted private key is refused rather than prompted for: there is no terminal here, and a
// passphrase the controller cannot supply would make every machine unreachable.
func AuthorizedKey(secret *corev1.Secret) (string, error) {
	key, err := PrivateKey(secret)
	if err != nil {
		return "", err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("unable to parse the private key in %s/%s (an encrypted key cannot be used): %w",
			secret.Namespace, secret.Name, err)
	}

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}
