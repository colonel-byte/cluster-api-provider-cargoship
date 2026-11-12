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

// Package manager holds the flags, scheme and manager setup both provider binaries share.
//
// They are two binaries rather than one because clusterctl installs them as two providers, with
// separate component manifests, so that an operator can run the bootstrap provider alongside a
// different control plane. Everything below that split is identical, and lives here.
package manager

import (
	"flag"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	bootstrapv1 "github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/api/v1alpha1"
	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
)

// Options are the flags both binaries take.
type Options struct {
	// MetricsAddr is where metrics are served. "0" disables.
	MetricsAddr string
	// ProbeAddr is where the health and readiness probes are served.
	ProbeAddr string
	// EnableLeaderElection runs only one active replica.
	EnableLeaderElection bool
	// LeaderElectionID is the lease name. The two binaries must not share one.
	LeaderElectionID string
	// WatchNamespace limits the manager to one namespace. Empty watches all.
	WatchNamespace string
	// SyncPeriod is the resync interval.
	//
	// Long on purpose. This provider's own requeues are what drive it forward; a short resync
	// would add nothing but reconciles that find the fingerprint unchanged and go back to sleep.
	SyncPeriod time.Duration
	// CachePath is where pulled OCI blobs are kept. Control-plane binary only.
	CachePath string
	// TempPath is where a distro package is extracted. Control-plane binary only.
	TempPath string
}

// BindFlags registers the shared flags on fs.
func (o *Options) BindFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "address the metric endpoint binds to, or 0 to disable")
	fs.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "address the probe endpoint binds to")
	fs.BoolVar(&o.EnableLeaderElection, "leader-elect", false, "run only one active replica")
	fs.StringVar(&o.WatchNamespace, "namespace", "", "namespace to watch, or empty for all")
	fs.DurationVar(&o.SyncPeriod, "sync-period", 30*time.Minute, "resync interval")
}

// BindConvergeFlags registers the flags only the control-plane binary takes.
func (o *Options) BindConvergeFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.CachePath, "cache-path", "/var/lib/cargoship/cache",
		"directory for pulled OCI blobs; must be writable and should outlive the pod")
	fs.StringVar(&o.TempPath, "temp-path", "/var/lib/cargoship/tmp",
		"directory a distro package is extracted into; needs room for one whole package")
}

// Scheme returns the scheme both binaries use.
func Scheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		clusterv1.AddToScheme,
		bootstrapv1.AddToScheme,
		controlplanev1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			return nil, fmt.Errorf("unable to build the scheme: %w", err)
		}
	}
	return scheme, nil
}

// New builds a manager from the options.
func New(o Options) (ctrl.Manager, error) {
	scheme, err := Scheme()
	if err != nil {
		return nil, err
	}

	opts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: o.MetricsAddr},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: o.ProbeAddr,
		LeaderElection:         o.EnableLeaderElection,
		LeaderElectionID:       o.LeaderElectionID,
		Cache: cache.Options{
			SyncPeriod: &o.SyncPeriod,
		},
	}
	if o.WatchNamespace != "" {
		opts.Cache.DefaultNamespaces = map[string]cache.Config{
			o.WatchNamespace: {},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), opts)
	if err != nil {
		return nil, fmt.Errorf("unable to start the manager: %w", err)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("unable to set up the health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("unable to set up the readiness check: %w", err)
	}
	return mgr, nil
}

// SetupLogging routes controller-runtime's logging and returns the zap flag set to register.
func SetupLogging(fs *flag.FlagSet) func() {
	zapOpts := zap.Options{Development: false}
	zapOpts.BindFlags(fs)
	return func() {
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	}
}

// Run starts the manager and blocks, exiting non-zero if it fails.
func Run(mgr ctrl.Manager) {
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "the manager stopped with an error")
		os.Exit(1)
	}
}
