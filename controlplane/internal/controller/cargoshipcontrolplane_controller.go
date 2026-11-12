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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

// CargoshipControlPlaneReconciler converges a fleet to a cargoship package.
type CargoshipControlPlaneReconciler struct {
	client.Client
	// Runner holds the in-flight converge. One per manager, not one per reconcile.
	Runner *converge.Runner
	// Probe reports whether a host is accepting SSH. Replaced in tests.
	Probe Prober
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=cargoshipcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=cargoshipcontrolplanes/status;cargoshipcontrolplanes/finalizers;cargoshipcontrolplanes/scale,verbs=get;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=cargoshipconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

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
	source, err := distroSource(cp)
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
		Apply:         applyRaw,
	}.Fingerprint()
	if err != nil {
		return ctrl.Result{}, err
	}

	// 6. Nothing to do: the fleet matches the last successful converge and every machine has
	//    joined. Requeue slowly to catch drift.
	if cp.Status.LastAppliedHash == fingerprint && allJoined(machines) {
		r.updateStatusCounts(cp, controlPlaneMachines)
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionTrue, "UpToDate",
			"the fleet matches the desired state")
		return ctrl.Result{RequeueAfter: r.driftInterval(cp)}, nil
	}

	// 7. Read what the registry needs, and start or observe the converge.
	//
	//    Both are done here rather than beside the SSH key. The pull secret writes a file, and a
	//    cluster that is already converged resyncs for all sorts of reasons and has no business
	//    rewriting it; the CA bundle is read in the reconcile rather than inside the converge so
	//    that a missing secret is an error on the resource instead of a failure minutes later.
	if err := r.writePullSecret(ctx, cp); err != nil {
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "PullSecretUnusable", err.Error())
		return ctrl.Result{}, err
	}

	caBundle, err := r.caBundle(ctx, cp)
	if err != nil {
		setCondition(cp, controlplanev1.ConvergedCondition, metav1.ConditionFalse, "CABundleUnusable", err.Error())
		return ctrl.Result{}, err
	}

	key := client.ObjectKeyFromObject(cp).String()
	run := r.Runner.Start(ctx, key, fingerprint, func(runCtx context.Context) (*converge.Run, error) {
		return converge.Apply(runCtx, inv, converge.Options{
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
			fmt.Sprintf("converging %d hosts", len(inv.Hosts)))
		return ctrl.Result{RequeueAfter: waitForInfrastructure}, nil

	case converge.StateFailed:
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
		r.updateStatusCounts(cp, controlPlaneMachines)
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

// caBundleField is the key a CA bundle is conventionally kept under in a secret -- the same one
// Kubernetes uses for the cluster's own certificate in a service account's token secret.
const caBundleField = "ca.crt"

// caBundle returns the certificates spec.distro.caBundleSecretRef names, and nil when the field is
// unset, in which case the pull trusts the controller's own roots and nothing more.
func (r *CargoshipControlPlaneReconciler) caBundle(
	ctx context.Context,
	cp *controlplanev1.CargoshipControlPlane,
) ([]byte, error) {
	if cp.Spec.Distro.CABundleSecretRef == nil {
		return nil, nil
	}

	key := types.NamespacedName{
		Namespace: cp.Namespace,
		Name:      cp.Spec.Distro.CABundleSecretRef.Name,
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

	key := types.NamespacedName{
		Namespace: cp.Namespace,
		Name:      cp.Spec.Distro.PullSecretRef.Name,
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
	key := types.NamespacedName{
		Namespace: cp.Namespace,
		Name:      cp.Spec.SSH.PrivateKeySecretRef.Name,
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
	// Scale up and down are the only changes this provider makes, so a machine that exists is a
	// machine at the desired state. When rollout lands this stops being true and has to compare
	// each machine against the desired version.
	cp.Status.UpToDateReplicas = cp.Status.Replicas
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
