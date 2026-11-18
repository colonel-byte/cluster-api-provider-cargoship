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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/distro"
	// Aliased because Apply holds the extracted package in a variable of the same name, and a
	// package shadowed by a local is a trap for the next edit rather than a compile error.
	pkglayout "github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/signing"
	"github.com/zarf-dev/zarf/src/pkg/logger"

	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/dockerconfig"
)

// Options tune the apply a converge runs. They mirror the subset of cargoship's own apply options
// the custom resource exposes.
type Options struct {
	// Source is the package to converge to, pinned to a digest where one is known.
	Source string
	// Architecture is the package architecture to pull. Empty takes the package's sole one.
	Architecture string
	// CABundle is PEM-encoded certificates to trust for the pull, in addition to the controller's
	// own roots rather than instead of them. This is how a registry serving a certificate nothing
	// public signed is pulled from without giving up verification.
	CABundle []byte
	// SignatureKey is the PEM-encoded Cosign public key the package's signature is verified
	// against. Empty verifies nothing: cargoship's default warns that it had no key to check a
	// signature against and loads the package anyway, signed or not.
	//
	// A key makes verification a precondition: cargoship is given VerifyAlways alongside it, so
	// an unsigned package, a bad signature, and a signature in a form it cannot check are all
	// fatal before the package is extracted.
	SignatureKey []byte
	// InsecureSkipTLSVerify accepts the registry's certificate without verifying it, rather than
	// trusting it through CABundle.
	InsecureSkipTLSVerify bool
	// Concurrency caps how many hosts a phase acts on at once. Zero is unlimited.
	Concurrency int
	// WorkerConcurrent is the worker batch size, a count ("5") or a percentage ("25%").
	WorkerConcurrent string
	// ModifyHosts lets cargoship rewrite each host's /etc/hosts from the inventory.
	ModifyHosts bool
	// ModifyFirewall lets cargoship open the cluster's ports in each host's own firewall.
	ModifyFirewall bool
	// LabelNodes adds the node-role label from each host's profile.
	LabelNodes bool
	// AllowDowngrade continues against a host already running a newer version than the package.
	AllowDowngrade bool
	// Timeout bounds the retry loops inside each phase. Zero takes cargoship's own default.
	Timeout time.Duration
}

// Paths are the two directories cargoship needs to be writable.
//
// They are set as process-wide globals rather than passed per call, which is cargoship's own
// shape, so a controller sets them once at startup rather than per reconcile. There is nothing
// else: an apply writes no kubeconfig when asked not to, writes no run receipt at all, and its
// cluster lock lives on the remote hosts.
type Paths struct {
	// Cache is where pulled OCI blobs are kept between reconciles. It must be set explicitly:
	// cargoship's default is "~/.cargoship-cache", and a container with no HOME resolves that to
	// a literal "~" directory.
	Cache string
	// Temp is where a package is extracted. One extracted package is as large as the package, so
	// this wants real storage rather than the pod's writable layer.
	Temp string
}

// SetPaths points cargoship's cache and temp directories at a mounted volume.
func SetPaths(p Paths) error {
	if p.Cache == "" || p.Temp == "" {
		return errors.New("both the cache and temp paths must be set")
	}
	if err := os.MkdirAll(p.Cache, 0o750); err != nil {
		return fmt.Errorf("unable to create the cache directory: %w", err)
	}
	if err := os.MkdirAll(p.Temp, 0o750); err != nil {
		return fmt.Errorf("unable to create the temp directory: %w", err)
	}
	config.CommonOptions.CachePath = p.Cache
	config.CommonOptions.TempDirectory = p.Temp

	// Registry credentials for a pull are read from $DOCKER_CONFIG, which is unset in the
	// container and would otherwise resolve to a HOME the image does not have. Pointing it inside
	// the cache puts the file on the same mounted volume as the blobs it authenticates.
	return dockerconfig.Use(filepath.Join(p.Cache, "docker"))
}

// Apply loads the package and converges the inventory to it.
//
// It returns a Run whether or not the apply succeeded, because cargoship's own result is worth
// reporting either way: a failed run still says which phases ran and which hosts they changed, and
// a controller that threw that away on error would leave an operator with a condition message and
// nothing else.
//
// The kubeconfig is built but never merged into a file. ApplyOptions.NoKubeConfigFile exists for
// exactly this: the credentials come back through KubeConfigBytes, and the machine running the
// apply -- here, a pod -- is not changed.
func Apply(ctx context.Context, inv *Inventory, opts Options) (*Run, error) {
	if config.CommonOptions.CachePath == "" || config.CommonOptions.TempDirectory == "" {
		return nil, errors.New("SetPaths has not been called")
	}

	// Everything cargoship logs it logs through logger.From(ctx), which discards when nothing is
	// attached. Attached here rather than around the apply alone because the package pull and the
	// manager build below log too, and those are the minutes an operator is waiting through when
	// a registry is slow or a signature does not verify.
	ctx = logger.WithContext(ctx, logger.Default())

	cachePath, err := config.GetAbsCachePath()
	if err != nil {
		return nil, fmt.Errorf("unable to resolve the cache path: %w", err)
	}

	loadOptions := distro.LoadOptions{
		CachePath:    cachePath,
		Architecture: opts.Architecture,
	}
	loadOptions.CABundle = opts.CABundle
	// Set after the literal because it is a promoted field: Go has no composite-literal syntax
	// for a field of an embedded struct, and naming cargoship's embedded types.RemoteOptions here
	// would couple this call to zarf's type rather than to cargoship's option struct.
	loadOptions.InsecureSkipTLSVerify = opts.InsecureSkipTLSVerify

	if len(opts.SignatureKey) > 0 {
		keyPath, removeKey, err := writeSignatureKey(opts.SignatureKey)
		if err != nil {
			return nil, err
		}
		defer removeKey()

		// From the library's defaults rather than a bare literal, so the verify keeps cargoship's
		// own timeout instead of none at all.
		verify := signing.DefaultVerifyBlobOptions()
		verify.Key = keyPath
		loadOptions.VerifyBlobOptions = &verify
		// Set as well as the key, which looks redundant and is not. A key alone already refuses
		// an unsigned package and a signature that does not verify, but a package signed by the
		// keyless method reports "nothing to verify against" -- which the default strategy
		// tolerates with a warning. That is a converge of unverified contents on a fleet whose
		// operator asked for verification, so the strategy says always.
		loadOptions.VerificationStrategy = pkglayout.VerifyAlways
	}

	layout, err := distro.Load(ctx, opts.Source, loadOptions)
	if err != nil {
		return nil, fmt.Errorf("unable to load the distro package %s: %w", opts.Source, err)
	}
	// The manager owns the extracted package from here, and the caller of distro.Load owns
	// removing it. A converge that left one behind per reconcile would fill the volume in a day.
	defer func() {
		// Not joined into the returned error: a converge that worked should not be reported as
		// failed because a temp directory would not go, and the next reconcile re-extracts into
		// a fresh one either way. What it does risk is filling the volume, which is why the
		// deployment sets a sizeLimit.
		_ = os.RemoveAll(layout.DirPath()) //nolint:errcheck // see above
	}()

	manager, err := action.NewManager(ctx, action.ManagerOptions{
		Cluster:     inv.Cluster,
		Layout:      layout,
		Concurrency: opts.Concurrency,
		Timeout:     opts.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to build the phase manager: %w", err)
	}

	apply, err := action.NewApply(action.ApplyOptions{
		Manager:          manager,
		AllowDowngrade:   opts.AllowDowngrade,
		ModifyHosts:      opts.ModifyHosts,
		ModifyFirewall:   opts.ModifyFirewall,
		WorkerConcurrent: opts.WorkerConcurrent,
		LabelNodes:       opts.LabelNodes,
		UpdateKubeConfig: true,
		NoKubeConfigFile: true,
		// No option exposes this, on purpose. DetectRemovedHosts stops an apply when the
		// cluster holds a node no host in the inventory accounts for, and a genuinely stray
		// node is worth stopping on. The reason it does not fire during an ordinary scale-down
		// is the pre-terminate hook: Cluster API drains the node and deletes the Node object
		// before the hook releases the instance, so by the next converge there is nothing
		// unaccounted for. If this starts firing during normal scale-down the hook is broken --
		// fix the hook rather than reaching for a flag to silence the check.
		AllowUnmanagedNodes: false,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to build the apply: %w", err)
	}

	run := &Run{}
	applyErr := apply.Run(ctx)
	run.Result = manager.Result

	if applyErr != nil {
		run.State = StateFailed
		run.Err = applyErr
		return run, applyErr
	}

	kubeconfig, err := apply.KubeConfigBytes()
	if err != nil {
		// The fleet converged; only the credentials could not be read back. Reporting that as a
		// failed converge would hide the part that worked, and the next reconcile retries it.
		run.State = StateFailed
		run.Err = fmt.Errorf("the converge succeeded but the admin credentials could not be read: %w", err)
		return run, run.Err
	}

	run.State = StateSucceeded
	run.KubeConfig = kubeconfig
	return run, nil
}

// writeSignatureKey puts the public key where cargoship can read it, and returns the function that
// takes it away again.
//
// A file because that is the only shape the library takes: signing.VerifyBlobOptions.Key is a
// path. This is not the exception to the SSH key never reaching a filesystem -- see
// docs/agent/choice-in-memory-ssh-key.md -- it is the public half of a signing keypair, which a
// file discloses nothing by holding. Not the env:// reference cargoship also accepts, which looks
// like the way to avoid a file and is not: an environment variable is process-wide, and two
// clusters converging at once in one manager would each overwrite the other's key.
//
// It is written into the directory packages are extracted into,
// so it lands on the mounted volume rather than the pod's writable layer, and the caller's defer
// takes it away when the apply returns -- not when the load does, so it outlives the pull it is
// for by the length of the converge.
func writeSignatureKey(pem []byte) (string, func(), error) {
	f, err := os.CreateTemp(config.CommonOptions.TempDirectory, "cosign-*.pub")
	if err != nil {
		return "", nil, fmt.Errorf("unable to create a file for the signature key: %w", err)
	}
	// Nothing useful to do with either error, and reporting one would fail a converge that
	// worked. The name is random per call and nothing sweeps the directory, so a manager killed
	// mid-converge does leave one behind -- a few hundred bytes of public key against the
	// volume's sizeLimit, next to an extracted package thousands of times its size.
	remove := func() { _ = os.Remove(f.Name()) } //nolint:errcheck // see above

	if _, err := f.Write(pem); err != nil {
		_ = f.Close() //nolint:errcheck // the write error is the one worth reporting
		remove()
		return "", nil, fmt.Errorf("unable to write the signature key: %w", err)
	}
	if err := f.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("unable to write the signature key: %w", err)
	}
	return f.Name(), remove, nil
}
