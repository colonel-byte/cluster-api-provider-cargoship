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

// Package controller holds the control-plane provider's reconciler.
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/zarfstate"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/converge"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/dockerconfig"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/sshkey"
)

// How long to wait before looking again, per situation.
//
// None of these is a deadline. Every one of them is "nothing to do yet, ask again shortly", which
// is the whole of this provider's answer to machines and API servers coming up on their own
// schedules: there is no step anywhere that gives up because something took too long.
const (
	// waitForInfrastructure is used while machines are being created and getting addresses, and
	// while a converge is in flight. Short, because these are the states an operator watches.
	waitForInfrastructure = 15 * time.Second
	// defaultDriftInterval is how often a converged fleet is re-converged to catch drift.
	defaultDriftInterval = 10 * time.Minute
	// waitForOwner is how long to wait for a reference another controller is about to write --
	// here, Cluster API setting itself as the control plane's owner.
	waitForOwner = 2 * time.Second
)

// ConvergeFunc is the apply itself, as the reconcile calls it.
//
// It exists so a test can drive the whole state machine without a fleet to SSH into. The one
// implementation outside tests is converge.Apply, which SetupWithManager installs.
type ConvergeFunc func(ctx context.Context, inv *converge.Inventory, opts converge.Options) (*converge.Run, error)

// ZarfStateFunc reads the registry half of a Zarf deployment's state, and the certificate
// authority for that registry where the cluster holds one.
//
// One function for both because a caller wants both or neither, and because it lets a test stand
// in for a Zarf deployment without one. The one implementation outside tests is zarfState, which
// SetupWithManager installs.
type ZarfStateFunc func(ctx context.Context, opts zarfstate.Options) (zarfstate.Registry, []byte, error)

// CargoshipControlPlaneReconciler converges a fleet to a cargoship package.
type CargoshipControlPlaneReconciler struct {
	client.Client
	// Runner holds the in-flight converge. One per manager, not one per reconcile.
	Runner *converge.Runner
	// Probe reports whether a host is accepting SSH. Replaced in tests.
	Probe Prober
	// Converge is the apply the Runner runs. Replaced in tests.
	Converge ConvergeFunc
	// Records reads what a registry holds, for a spec.distro that names a package rather than
	// referencing one. Replaced in tests.
	Records RecordLister
	// ZarfState reads a Zarf deployment's registry credentials, for a spec.distro that asks for
	// them. Replaced in tests.
	ZarfState ZarfStateFunc
	// SecretNamespaces are the namespaces other than a control plane's own that it may name a
	// secret in. Empty refuses every cross-namespace reference; see the flag's comment in
	// pkg/manager for why that is the default.
	SecretNamespaces []string
	// RESTConfig is the connection ZarfState reads over. It is the manager's own, so the
	// credentials are the controller's service account rather than a kubeconfig it does not have.
	RESTConfig *rest.Config
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=cargoshipcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=cargoshipcontrolplanes/status;cargoshipcontrolplanes/finalizers;cargoshipcontrolplanes/scale,verbs=get;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=cargoshipconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// Every infrastructure provider's machine kind, because the infrastructure machine each Machine
// gets is cloned from whatever template the control plane names and that kind is not known here.
// The consequence, stated plainly rather than left to be worked out: this manager can write every
// object in infrastructure.cluster.x-k8s.io in the cluster, not only the ones it clones.
// KubeadmControlPlane carries the same grant for the same reason, so narrowing it would need a
// mechanism Cluster API does not have rather than a tighter marker here.
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile walks the control plane one step towards its desired state.
//
// It never blocks on the fleet. Each pass either finds something to do and does it, or finds
// something to wait for and requeues; the one long operation -- the cargoship apply, which walks
// every host over SSH and takes minutes -- is started on the Runner and observed by the passes
// that follow. See the type comment on converge.Runner for why that cannot be a requeue.
func (r *CargoshipControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	cp := &controlplanev1.CargoshipControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, r.Client, cp.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		logger.V(1).Info("waiting for the owning Cluster to be set")
		return ctrl.Result{RequeueAfter: waitForOwner}, nil
	}

	patchHelper, err := patch.NewHelper(cp, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer func() {
		if err := patchHelper.Patch(ctx, cp); err != nil && reterr == nil {
			reterr = err
		}
	}()

	cp.Status.ObservedGeneration = cp.Generation
	cp.Status.Selector = machineSelector(cluster.Name, cp.Name).String()

	if !cp.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cluster, cp)
	}
	controllerutil.AddFinalizer(cp, controlplanev1.ControlPlaneFinalizer)

	if !ptr.Deref(cluster.Status.Initialization.InfrastructureProvisioned, false) {
		logger.V(1).Info("waiting for the cluster infrastructure")
		setCondition(cp, controlplanev1.AvailableCondition, metav1.ConditionFalse,
			"WaitingForInfrastructure", "the cluster infrastructure is not provisioned yet")
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil
	}

	return r.reconcileNormal(ctx, cluster, cp)
}

// reconcileNormal is the happy path, in the order the steps have to happen in.
func (r *CargoshipControlPlaneReconciler) reconcileNormal(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	cp *controlplanev1.CargoshipControlPlane,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Every machine in the cluster, control plane and MachineDeployment workers alike. This is
	// the departure from KubeadmControlPlane, and the reason for it is that cargoship converges a
	// whole fleet in one run: the hosts share an /etc/hosts file and a firewall, and two
	// uncoordinated SSH walks over them would fight. See docs/agent/choice-one-converge.md.
	machines, err := r.clusterMachines(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	controlPlaneMachines := filterControlPlane(machines)

	// 1. Bring the control-plane machine count to spec.replicas, one machine per pass so each
	//    creation is observed before the next.
	if result, done, err := r.reconcileReplicas(ctx, cluster, cp, controlPlaneMachines); err != nil || !done {
		return result, err
	}

	// 2. The pre-terminate hook, on every machine we manage. It has to be in place before a
	//    deletion rather than at deletion time, because by then the machine is already being
	//    drained and there is no window left to claim.
	if err := r.ensurePreTerminateHooks(ctx, controlPlaneMachines); err != nil {
		return ctrl.Result{}, err
	}

	// 3. Build the inventory from the machines that are reachable now. One without an address is
	//    skipped rather than waited for.
	endpoint := r.endpoint(cp, controlPlaneMachines)
	if endpoint == "" {
		logger.V(1).Info("waiting for a control-plane machine to get an address")
		setCondition(cp, controlplanev1.MachinesReadyCondition, metav1.ConditionFalse,
			"WaitingForAddress", "no control-plane machine has an address yet")
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil
	}

	inv, clusterConfigRaw, err := r.buildInventory(ctx, cluster, cp, machines, endpoint)
	if err != nil {
		setCondition(cp, controlplanev1.MachinesReadyCondition, metav1.ConditionFalse, "InventoryIncomplete", err.Error())
		// Not an error to escalate: "no machine has an address yet" is the ordinary state of a
		// cluster being created, and returning it as an error would log a stack trace per pass.
		logger.V(1).Info("inventory not ready", "reason", err.Error())
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil
	}
	if len(inv.Skipped) > 0 {
		logger.V(1).Info("some machines have no address yet", "skipped", inv.Skipped)
	}
	setCondition(cp, controlplanev1.MachinesReadyCondition, metav1.ConditionTrue, "HostsResolved",
		fmt.Sprintf("%d of %d machines are addressable", len(inv.Hosts), len(machines)))

	// 4. Is the leader even accepting SSH? A dial with a short timeout, not a phase: cargoship's
	//    own retry would sit inside the apply for fifteen minutes waiting for a machine that is
	//    still booting, holding the runner slot and telling the operator nothing.
	leader := firstController(inv)
	if ok, err := r.Probe.Reachable(ctx, leader); err != nil || !ok {
		reason := "the control-plane machine is not accepting ssh yet"
		if err != nil {
			reason = err.Error()
		}
		logger.V(1).Info("waiting for ssh", "host", leader.Address, "reason", reason)
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "WaitingForSSH", reason)
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil
	}

	// 5. Resolve the package reference, and record it. See distroSource for what a tagged
	//    reference does and does not get you.
	//
	//    A spec.distro naming a package rather than referencing one has to read the registry to
	//    resolve it, so the credentials and the CA that step 7 otherwise stages for the converge
	//    are staged here instead. Only that form pays for it: a resync of a converged cluster
	//    then rewrites a Docker configuration file it had no reason to touch, which is the price
	//    of resolving a name at all.
	var caBundle []byte
	staged := cp.Spec.Distro.Name != ""
	if staged {
		if caBundle, err = r.stageRegistryAccess(ctx, cp); err != nil {
			return ctrl.Result{}, err
		}
	}

	source, err := distroSource(ctx, cp, r.Records, caBundle)
	if err != nil {
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "DistroUnresolved", err.Error())
		return ctrl.Result{}, err
	}

	applyRaw, err := json.Marshal(cp.Spec.Apply)
	if err != nil {
		return ctrl.Result{}, err
	}
	fingerprint, err := converge.DesiredState{
		Hosts:         inv.DesiredHosts(),
		ClusterConfig: clusterConfigRaw,
		Endpoint:      endpoint,
		DistroRef:     source,
		// Only when an operator asked for it. See AdoptRepublishedPackageAnnotation: the digest
		// is read and reported either way, and folding it in is what turns a republished package
		// into a converge nobody triggered.
		DistroDigest: adoptRepublished(cp),
		Apply:        applyRaw,
	}.Fingerprint()
	if err != nil {
		return ctrl.Result{}, err
	}

	// 6. A version change gets a person's attention before it gets a converge.
	//
	//    After the fingerprint rather than before it, because the fingerprint is what says
	//    whether the change could even take effect -- and after resolution, so status.distro
	//    still reports what the registry holds for an operator deciding what to do about it.
	if from, changing := versionChange(cp); changing {
		switch {
		case cp.Status.LastAppliedHash == fingerprint:
			// A version with no package behind it. The fleet runs what the package installs,
			// so this one changes nothing and no approval would make it change anything --
			// worth its own answer, because the alternative is a resource that reports itself
			// converged at a version it is not running.
			setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "VersionChangeInert",
				fmt.Sprintf("spec.version changed from %s to %s, but spec.distro still names the package "+
					"installing %s, so a converge would change nothing. Point spec.distro at a package "+
					"that installs %s.", from, cp.Spec.Version, from, cp.Spec.Version))
			r.updateStatusCounts(cp, controlPlaneMachines, false)
			logger.V(1).Info("version change names no new package", "from", from, "to", cp.Spec.Version)
			return ctrl.Result{RequeueAfter: r.driftInterval(cp)}, nil

		case cp.Annotations[controlplanev1.ApproveVersionChangeAnnotation] != cp.Spec.Version:
			setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "VersionChangeRefused",
				fmt.Sprintf("spec.version changed from %s to %s, and this provider has no rollout: "+
					"a converge would upgrade every host in place, unsupervised. "+
					"Set the annotation %s=%s to approve it.",
					from, cp.Spec.Version, controlplanev1.ApproveVersionChangeAnnotation, cp.Spec.Version))
			r.updateStatusCounts(cp, controlPlaneMachines, false)
			// Not an error: it is a decision waiting on a person, and an error here would log a
			// stack trace per retry and back off a reconcile that has nothing to retry.
			logger.V(1).Info("version change not approved", "from", from, "to", cp.Spec.Version)
			return ctrl.Result{RequeueAfter: r.driftInterval(cp)}, nil
		}
	}

	// 7. Nothing to do: the fleet matches the last successful converge and every machine has
	//    joined. Requeue slowly to catch drift.
	if cp.Status.LastAppliedHash == fingerprint && allJoined(machines) {
		r.updateStatusCounts(cp, controlPlaneMachines, true)
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionTrue, "UpToDate",
			"the fleet matches the desired state")
		return ctrl.Result{RequeueAfter: r.driftInterval(cp)}, nil
	}

	// 8. Read what the registry needs, and start or observe the converge.
	//
	//    Both are done here rather than beside the SSH key. The pull secret writes a file, and a
	//    cluster that is already converged resyncs for all sorts of reasons and has no business
	//    rewriting it; the CA bundle is read in the reconcile rather than inside the converge so
	//    that a missing secret is an error on the resource instead of a failure minutes later.
	if !staged {
		if caBundle, err = r.stageRegistryAccess(ctx, cp); err != nil {
			return ctrl.Result{}, err
		}
	}

	key := client.ObjectKeyFromObject(cp).String()
	run := r.Runner.Start(ctx, key, fingerprint, func(runCtx context.Context) (*converge.Run, error) {
		return r.Converge(runCtx, inv, converge.Options{
			Source:                source,
			Architecture:          cp.Spec.Distro.Arch,
			CABundle:              caBundle,
			InsecureSkipTLSVerify: cp.Spec.Distro.InsecureSkipTLSVerify,
			Concurrency:           int(cp.Spec.Apply.Concurrency),
			WorkerConcurrent:      cp.Spec.Apply.WorkerConcurrent,
			ModifyHosts:           cp.Spec.Apply.ModifyHosts,
			ModifyFirewall:        cp.Spec.Apply.ModifyFirewall,
			LabelNodes:            cp.Spec.Apply.LabelNodes,
			AllowDowngrade:        cp.Spec.Apply.AllowDowngrade,
			Timeout:               time.Duration(cp.Spec.Apply.TimeoutSeconds) * time.Second,
		})
	})

	switch run.State {
	case converge.StateRunning:
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "Converging",
			convergingMessage(len(inv.Hosts), run))
		r.updateStatusCounts(cp, controlPlaneMachines, false)
		logger.V(1).Info("converging", "hosts", len(inv.Hosts), "phase", run.Progress.Phase,
			"index", run.Progress.Index, "total", run.Progress.Total)
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil

	case converge.StateFailed:
		logger.Error(run.Err, "converge failed")
		r.recordHosts(cp, inv, run)
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "ConvergeFailed", run.Err.Error())
		// Forgotten so the next pass runs again rather than reporting the same failure forever.
		// The backoff comes from the requeue, not from the runner.
		if err := r.Runner.Forget(key); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil

	case converge.StateSucceeded:
		if err := r.writeKubeConfig(ctx, cluster, cp, run.KubeConfig); err != nil {
			setCondition(cp, controlplanev1.AvailableCondition, metav1.ConditionFalse, "KubeConfigUnavailable", err.Error())
			return ctrl.Result{}, err
		}

		// The survivors have now been converged without the departing hosts in the inventory,
		// so their /etc/hosts and firewall rules no longer name a machine that is going. That
		// is exactly the window the pre-terminate hook was held open for, so release it and let
		// the infrastructure provider destroy the instances.
		if err := r.releasePreTerminateHooks(ctx, machines); err != nil {
			return ctrl.Result{}, err
		}

		r.recordHosts(cp, inv, run)
		cp.Status.LastAppliedHash = fingerprint
		cp.Status.Version = cp.Spec.Version
		// The contract field that releases worker machines to be created. It is set once the
		// credentials exist, which is the first moment anything could have talked to the API.
		cp.Status.Initialization.ControlPlaneInitialized = ptr.To(true)
		r.updateStatusCounts(cp, controlPlaneMachines, true)
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionTrue, "Converged",
			fmt.Sprintf("converged %d hosts", len(inv.Hosts)))
		setCondition(cp, controlplanev1.AvailableCondition, metav1.ConditionTrue, "Available",
			"the control plane is answering")

		if err := r.Runner.Forget(key); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("converged", "hosts", len(inv.Hosts), "changed", run.Result.Changed())
		return ctrl.Result{RequeueAfter: r.driftInterval(cp)}, nil
	}

	return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil
}

// convergingMessage is how far the in-flight apply has got, for the Converged condition while it
// runs.
//
// It is the only thing that changes about a converging resource for minutes at a time, so it is
// what an operator reads to tell a slow phase from a stuck one. The phase comes from cargoship's
// progress sink, which is empty until the apply enters its first phase.
func convergingMessage(hosts int, run *converge.Run) string {
	if run.Progress.Phase == "" {
		return fmt.Sprintf("converging %d hosts", hosts)
	}
	return fmt.Sprintf("converging %d hosts: phase %d of %d, %s",
		hosts, run.Progress.Index, run.Progress.Total, run.Progress.Phase)
}

// caBundleField is the key a CA bundle is conventionally kept under in a secret -- the same one
// Kubernetes uses for the cluster's own certificate in a service account's token secret.
const caBundleField = "ca.crt"

// adoptRepublished is the discovered package's content digest when the annotation asking a
// converge to follow a republished package is set, and empty otherwise.
//
// Empty rather than absent from the fingerprint, which is the same thing: the field is
// omitempty, so an unannotated resource hashes exactly as it did before discovery existed.
func adoptRepublished(cp *controlplanev1.CargoshipControlPlane) string {
	if cp.Annotations[controlplanev1.AdoptRepublishedPackageAnnotation] != "true" {
		return ""
	}
	return cp.Status.Distro.RecordDigest
}

// stageRegistryAccess puts the credentials and the certificates the registry needs in place, and
// returns the certificates for whatever is about to talk to it.
//
// The two are together because neither is any use without the other and both report themselves
// the same way: as a condition on the resource rather than as a failure minutes into a converge.
// It sets the condition and returns the error, so a caller only has to return it.
func (r *CargoshipControlPlaneReconciler) stageRegistryAccess(
	ctx context.Context,
	cp *controlplanev1.CargoshipControlPlane,
) ([]byte, error) {
	if cp.Spec.Distro.FromZarfState {
		return r.stageFromZarfState(ctx, cp)
	}

	if err := r.writePullSecret(ctx, cp); err != nil {
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "PullSecretUnusable", err.Error())
		return nil, err
	}

	caBundle, err := r.caBundle(ctx, cp)
	if err != nil {
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "CABundleUnusable", err.Error())
		return nil, err
	}
	return caBundle, nil
}

// stageFromZarfState does the same for a registry a Zarf deployment stood up, reading what it
// needs out of that deployment's own state rather than out of secrets an operator wrote.
//
// The credentials go into the same Docker configuration file a pull secret would, because that
// file is still the only place the pull reads them from. The listing that resolves the package
// reads it too -- distro.ListMirroredOptions falls back to it when handed no username -- so
// nothing has to be threaded through for either.
func (r *CargoshipControlPlaneReconciler) stageFromZarfState(
	ctx context.Context,
	cp *controlplanev1.CargoshipControlPlane,
) ([]byte, error) {
	registry, caBundle, err := r.ZarfState(ctx, zarfstate.Options{RESTConfig: r.RESTConfig})
	if err != nil {
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "ZarfStateUnreadable", err.Error())
		return nil, err
	}

	// Attempted rather than refused, which is the opposite of what the state's mTLS strategy
	// reads like. Zarf's registry is configured `clientauth: verify-client-cert-if-given`
	// (packages/init/.../zarf-registry/chart/templates/_helpers.tpl), so a client certificate is
	// verified where one is offered and never required: the certificate Zarf issues belongs to
	// the per-node registry proxy, which presents it on a node's behalf. A controller holding
	// only the CA connects. See docs/agent/choice-registry-credentials.md.
	if registry.UsesMTLS() {
		log.FromContext(ctx).V(1).Info("the zarf registry records an mTLS strategy; "+
			"connecting with its CA and no client certificate of our own",
			"mtlsStrategy", registry.MTLSStrategy)
	}

	// The pull-only pair, because reading records and pulling a package is all this does. Zarf
	// writes both pairs, so the fallback is for a state somebody assembled by hand rather than
	// for anything zarf produces.
	username, password := registry.PullUsername, registry.PullPassword
	if username == "" || password == "" {
		username, password = registry.PushUsername, registry.PushPassword
	}

	if err := dockerconfig.MergeCredential(registryHost(cp.Spec.Distro.Registry), username, password); err != nil {
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "ZarfStateUnusable", err.Error())
		return nil, err
	}
	return caBundle, nil
}

// zarfState reads a Zarf deployment's registry and the CA that signed its certificate.
//
// A missing CA is not a failure. The secret only exists where Zarf manages the registry's TLS, so
// a registry serving a publicly signed certificate -- an external one, usually -- has none, and
// there is nothing extra for the pull to trust.
func zarfState(ctx context.Context, opts zarfstate.Options) (zarfstate.Registry, []byte, error) {
	registry, err := zarfstate.Load(ctx, opts)
	if err != nil {
		return zarfstate.Registry{}, nil, err
	}

	caBundle, err := zarfstate.LoadCABundle(ctx, opts)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return zarfstate.Registry{}, nil, err
		}
		caBundle = nil
	}
	return registry, caBundle, nil
}

// registryHost is spec.distro.registry with any repository prefix taken off, which is how the
// Docker credential store keys an entry. See dockerconfig.MergeCredential.
func registryHost(registry string) string {
	host, _, _ := strings.Cut(strings.TrimSpace(registry), "/")
	return host
}

// caBundle returns the certificates spec.distro.caBundleSecretRef names, and nil when the field is
// unset, in which case the pull trusts the controller's own roots and nothing more.
func (r *CargoshipControlPlaneReconciler) caBundle(
	ctx context.Context,
	cp *controlplanev1.CargoshipControlPlane,
) ([]byte, error) {
	if cp.Spec.Distro.CABundleSecretRef == nil {
		return nil, nil
	}

	key, err := cp.Spec.Distro.CABundleSecretRef.Key(cp.Namespace, r.SecretNamespaces)
	if err != nil {
		return nil, err
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, key, secret); err != nil {
		return nil, fmt.Errorf("unable to read the ca bundle secret %s: %w", key, err)
	}

	bundle := secret.Data[caBundleField]
	if len(bundle) == 0 {
		return nil, fmt.Errorf("ca bundle secret %s has no %q field, or it is empty", key, caBundleField)
	}
	return bundle, nil
}

// writePullSecret merges the registry credentials spec.distro.pullSecretRef names into the Docker
// config file the pull reads, and does nothing when the field is unset.
//
// A missing or malformed secret is returned as an error rather than ignored. Falling back to
// anonymous access would turn "the credentials are wrong" into a pull that fails deep inside
// cargoship with a 401 against a registry the operator believed they had authenticated to.
func (r *CargoshipControlPlaneReconciler) writePullSecret(
	ctx context.Context,
	cp *controlplanev1.CargoshipControlPlane,
) error {
	if cp.Spec.Distro.PullSecretRef == nil {
		return nil
	}

	key, err := cp.Spec.Distro.PullSecretRef.Key(cp.Namespace, r.SecretNamespaces)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, key, secret); err != nil {
		return fmt.Errorf("unable to read the pull secret %s: %w", key, err)
	}

	payload, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return fmt.Errorf("pull secret %s has no %q field: it must be a kubernetes.io/dockerconfigjson secret",
			key, corev1.DockerConfigJsonKey)
	}
	if err := dockerconfig.Merge(payload); err != nil {
		return fmt.Errorf("unable to use the pull secret %s: %w", key, err)
	}
	return nil
}

// buildInventory decodes the cluster config and builds the cargoship inventory.
//
// It returns the raw config alongside the inventory because the fingerprint hashes the bytes the
// API server stored, not the decoded struct: a struct round-trip would drop any key cargoship
// gained in a later version and quietly stop noticing changes to it.
func (r *CargoshipControlPlaneReconciler) buildInventory(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	cp *controlplanev1.CargoshipControlPlane,
	machines []*clusterv1.Machine,
	endpoint string,
) (*converge.Inventory, []byte, error) {
	var raw []byte
	if cp.Spec.ClusterConfig != nil {
		raw = cp.Spec.ClusterConfig.Raw
	}

	clusterConfig, err := converge.DecodeClusterConfig(raw)
	if err != nil {
		return nil, nil, err
	}

	secret := &corev1.Secret{}
	key, err := cp.Spec.SSH.PrivateKeySecretRef.Key(cp.Namespace, r.SecretNamespaces)
	if err != nil {
		return nil, nil, err
	}
	if err := r.Get(ctx, key, secret); err != nil {
		return nil, nil, fmt.Errorf("unable to read the ssh key secret %s: %w", key, err)
	}
	privateKey, err := sshkey.PrivateKey(secret)
	if err != nil {
		return nil, nil, err
	}

	inv, err := converge.BuildInventory(converge.Input{
		ClusterName:   cluster.Name,
		Endpoint:      endpoint,
		ClusterConfig: clusterConfig,
		SSH: converge.SSHParams{
			User:       cp.Spec.SSH.User,
			Port:       int(cp.Spec.SSH.Port),
			PrivateKey: privateKey,
		},
		Machines: machines,
	})
	if err != nil {
		return nil, nil, err
	}
	return inv, raw, nil
}

// endpoint is the host clients reach the API through.
//
// A cluster that will ever hold more than one controller should set spec.controlPlaneEndpoint to
// something that survives a machine being replaced. Falling back to the first machine's address is
// right for a single-controller cluster and a trap for any other, so it is logged when it happens.
func (r *CargoshipControlPlaneReconciler) endpoint(
	cp *controlplanev1.CargoshipControlPlane,
	controlPlaneMachines []*clusterv1.Machine,
) string {
	if cp.Spec.ControlPlaneEndpoint.Host != "" {
		return cp.Spec.ControlPlaneEndpoint.Host
	}
	for _, m := range controlPlaneMachines {
		for _, t := range []clusterv1.MachineAddressType{clusterv1.MachineInternalIP, clusterv1.MachineExternalIP} {
			for _, a := range m.Status.Addresses {
				if a.Type == t && a.Address != "" {
					return a.Address
				}
			}
		}
	}
	return ""
}

// driftInterval is how long to wait before re-converging a fleet that already matches.
func (r *CargoshipControlPlaneReconciler) driftInterval(cp *controlplanev1.CargoshipControlPlane) time.Duration {
	if cp.Spec.Apply.DriftIntervalSeconds > 0 {
		return time.Duration(cp.Spec.Apply.DriftIntervalSeconds) * time.Second
	}
	return defaultDriftInterval
}

// recordHosts writes the per-host outcome of a converge onto the status.
func (r *CargoshipControlPlaneReconciler) recordHosts(
	cp *controlplanev1.CargoshipControlPlane,
	inv *converge.Inventory,
	run *converge.Run,
) {
	byAddress := map[string]converge.Host{}
	for _, h := range inv.Hosts {
		byAddress[h.Address] = h
	}

	out := make([]controlplanev1.HostStatus, 0, len(inv.Hosts))
	for _, summary := range run.Result.HostSummaries() {
		status := controlplanev1.HostStatus{
			Address: summary.Host,
			Changed: summary.Changed,
			// Reported as a pair: the count of phases that named this host is unreadable
			// without the count that reported per-host at all.
			PhasesNamed:     int32(summary.Named),     //nolint:gosec // a phase count cannot overflow
			PhasesReporting: int32(summary.Reporting), //nolint:gosec // a phase count cannot overflow
		}
		if h, ok := byAddress[summary.Host]; ok {
			status.Machine = h.Machine.Name
			status.Role = h.Role
		}
		out = append(out, status)
	}
	cp.Status.Hosts = out
}

// updateStatusCounts fills in the contract's replica counts.
func (r *CargoshipControlPlaneReconciler) updateStatusCounts(
	cp *controlplanev1.CargoshipControlPlane,
	controlPlaneMachines []*clusterv1.Machine,
	upToDate bool,
) {
	var ready, available int32
	for _, m := range controlPlaneMachines {
		if m.Status.NodeRef.Name != "" {
			ready++
		}
		if isAvailable(m) {
			available++
		}
	}

	cp.Status.Replicas = ptr.To(int32(len(controlPlaneMachines))) //nolint:gosec // a replica count cannot overflow
	cp.Status.ReadyReplicas = ptr.To(ready)
	cp.Status.AvailableReplicas = ptr.To(available)

	// Up to date is a property of the fleet's state rather than of a machine, because this
	// provider converges hosts in place instead of replacing them: a machine is as current as
	// the last converge that included it. So the caller passes whether the last successful
	// converge matches what is wanted now -- the fingerprint comparison it has already made --
	// and every machine is up to date or none is.
	//
	// It used to be the replica count unconditionally, which read as "everything is current"
	// while a version change sat unapproved or a converge was still running.
	if upToDate {
		cp.Status.UpToDateReplicas = cp.Status.Replicas
	} else {
		cp.Status.UpToDateReplicas = ptr.To(int32(0))
	}
}

// versionChange reports the version the last successful converge installed, when spec.version has
// moved away from it.
//
// status.version rather than the machines' own versions, because a machine's spec.version is
// fixed when it is created and this provider never replaces a machine -- so after an in-place
// upgrade the machines would disagree with the fleet forever, and a gate reading them would
// refuse every converge from then on. status.version is written only by a successful converge,
// so it is what the hosts last had installed.
func versionChange(cp *controlplanev1.CargoshipControlPlane) (string, bool) {
	from := cp.Status.Version
	// Empty is a control plane that has never converged, where there is nothing to have changed.
	if from == "" || from == cp.Spec.Version {
		return "", false
	}
	return from, true
}

// writeKubeConfig publishes the admin credentials as the secret Cluster API expects.
func (r *CargoshipControlPlaneReconciler) writeKubeConfig(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	cp *controlplanev1.CargoshipControlPlane,
	kubeconfig []byte,
) error {
	if len(kubeconfig) == 0 {
		return fmt.Errorf("the converge returned no admin credentials")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			// The name is fixed by convention, not chosen: everything from clusterctl to Rancher
			// Turtles looks for <cluster>-kubeconfig.
			Name:      fmt.Sprintf("%s-kubeconfig", cluster.Name),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: controlplanev1.GroupVersion.String(),
					Kind:       "CargoshipControlPlane",
					Name:       cp.Name,
					UID:        cp.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Data: map[string][]byte{"value": kubeconfig},
		Type: clusterv1.ClusterSecretType,
	}

	if err := r.Create(ctx, secret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("unable to create the kubeconfig secret: %w", err)
		}
		// Updated rather than left alone: the credentials are read fresh from the control plane
		// on each converge, and an endpoint that changed makes the stored copy wrong.
		if err := r.Update(ctx, secret); err != nil {
			return fmt.Errorf("unable to update the kubeconfig secret: %w", err)
		}
	}
	return nil
}

// SetupWithManager wires the reconciler up.
func (r *CargoshipControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Runner == nil {
		r.Runner = converge.NewRunner()
	}
	if r.Probe == nil {
		r.Probe = &DialProbe{}
	}
	if r.Converge == nil {
		r.Converge = converge.Apply
	}
	if r.Records == nil {
		r.Records = distro.ListMirrored
	}
	if r.ZarfState == nil {
		r.ZarfState = zarfState
	}
	if r.RESTConfig == nil {
		r.RESTConfig = mgr.GetConfig()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1.CargoshipControlPlane{}).
		Owns(&clusterv1.Machine{}).
		// Worker machines are not owned by this control plane but are part of its converge, so
		// nothing else would wake it when one appears, gets an address, or is deleted. This watch
		// is what makes a MachineDeployment scale-up join the fleet without waiting for the drift
		// interval.
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.controlPlanesForMachine),
		).
		Named("cargoshipcontrolplane").
		Complete(r)
}
