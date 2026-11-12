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

// Package controller holds the bootstrap provider's reconciler.
package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1 "github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/bootstrap/internal/cloudinit"
	controlplanev1 "github.com/colonel-byte/cluster-api-provider-cargoship/controlplane/api/v1alpha1"
	"github.com/colonel-byte/cluster-api-provider-cargoship/pkg/sshkey"
)

// waitForOwner is how long to wait for a reference another controller is about to write.
//
// Short, because these are states that resolve in one or two API round trips: the MachineSet or
// control plane creates the config and then sets itself as owner in a separate write. A bare
// `Requeue: true` would do, but it is deprecated in favour of saying how long.
const waitForOwner = 2 * time.Second

// CargoshipConfigReconciler renders one machine's bootstrap data.
//
// There is very little to it, and that is the design rather than an unfinished state: all this
// provider's bootstrap data does is make a machine reachable over SSH. See the package comment on
// bootstrap/internal/cloudinit.
type CargoshipConfigReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=cargoshipconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=cargoshipconfigs/status;cargoshipconfigs/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=cargoshipconfigtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=cargoshipcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile renders the bootstrap secret for a CargoshipConfig.
func (r *CargoshipConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	config := &bootstrapv1.CargoshipConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	machine, err := ownerMachine(ctx, r.Client, config)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		// Normal, and brief: the MachineSet or control plane creates the config and then sets
		// itself as the owner. Nothing to watch for, so a plain requeue is right.
		logger.V(1).Info("waiting for the owning Machine to be set")
		return ctrl.Result{RequeueAfter: waitForOwner}, nil
	}

	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer func() {
		if patchErr := patchHelper.Patch(ctx, config); patchErr != nil && err == nil {
			err = patchErr
		}
	}()

	config.Status.ObservedGeneration = config.Generation

	if !config.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(config, bootstrapv1.ConfigFinalizer)
		return ctrl.Result{}, nil
	}
	if controllerutil.AddFinalizer(config, bootstrapv1.ConfigFinalizer) {
		// The finalizer is only here so the owner reference on the secret is never the only
		// thing keeping it; garbage collection does the actual deletion.
		logger.V(1).Info("added the finalizer")
	}

	// Already rendered. The bootstrap data is write-once on purpose: on most infrastructure
	// providers it is user-data a running machine has already consumed, so rewriting it changes
	// nothing on the machine while making every reconcile look like a change. A config whose
	// spec changed after the machine booted needs a new machine, which is a rollout, which this
	// provider does not do yet.
	if config.Status.DataSecretName != "" {
		return ctrl.Result{}, nil
	}

	controlPlaneKey, err := controlPlaneRefFor(ctx, r.Client, machine)
	if err != nil {
		return ctrl.Result{}, err
	}

	controlPlane := &controlplanev1.CargoshipControlPlane{}
	if err := r.Get(ctx, controlPlaneKey, controlPlane); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("waiting for the CargoshipControlPlane", "controlPlane", controlPlaneKey)
			return ctrl.Result{RequeueAfter: waitForOwner}, nil
		}
		return ctrl.Result{}, err
	}

	// The control plane holds the key the controller will connect with, and this is where its
	// public half is authorized. Taking it from there rather than from the CargoshipConfig means
	// a worker's config cannot be written in a way that locks the controller out.
	controllerKey, err := r.publicKey(ctx, controlPlane)
	if err != nil {
		setCondition(config, bootstrapv1.DataSecretAvailableCondition, metav1.ConditionFalse, "SSHKeyUnavailable", err.Error())
		return ctrl.Result{}, err
	}

	user := config.Spec.User
	if user == "" {
		user = controlPlane.Spec.SSH.User
	}

	files, err := r.resolveFiles(ctx, config)
	if err != nil {
		setCondition(config, bootstrapv1.DataSecretAvailableCondition, metav1.ConditionFalse, "FilesUnavailable", err.Error())
		return ctrl.Result{}, err
	}

	data, err := cloudinit.Render(cloudinit.Config{
		User:           user,
		AuthorizedKeys: append([]string{controllerKey}, config.Spec.SSHAuthorizedKeys...),
		Files:          files,
		RunCmd:         config.Spec.PreCargoshipCommands,
	})
	if err != nil {
		// A config that cannot render will not render on a retry either, so this is terminal:
		// reported through failureReason so the Machine controller stops waiting on it.
		config.Status.FailureReason = "RenderFailed"
		config.Status.FailureMessage = err.Error()
		setCondition(config, bootstrapv1.DataSecretAvailableCondition, metav1.ConditionFalse, "RenderFailed", err.Error())
		return ctrl.Result{}, nil
	}

	secretName, err := r.writeSecret(ctx, config, machine, data)
	if err != nil {
		setCondition(config, bootstrapv1.DataSecretAvailableCondition, metav1.ConditionFalse, "SecretWriteFailed", err.Error())
		return ctrl.Result{}, err
	}

	config.Status.DataSecretName = secretName
	config.Status.Initialization.DataSecretCreated = ptr.To(true)
	setCondition(config, bootstrapv1.DataSecretAvailableCondition, metav1.ConditionTrue, "DataSecretCreated", "the bootstrap secret has been written")

	logger.Info("wrote the bootstrap secret", "secret", secretName, "machine", machine.Name)
	return ctrl.Result{}, nil
}

// publicKey derives the authorized-keys line from the control plane's private key secret.
//
// Deriving it rather than asking for it separately is one less thing to get out of step: a public
// key that did not match the private key the controller connects with would produce a fleet of
// machines that boot correctly and refuse every connection.
func (r *CargoshipConfigReconciler) publicKey(ctx context.Context, cp *controlplanev1.CargoshipControlPlane) (string, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: cp.Namespace,
		Name:      cp.Spec.SSH.PrivateKeySecretRef.Name,
	}
	if err := r.Get(ctx, key, secret); err != nil {
		return "", fmt.Errorf("unable to read the ssh key secret %s: %w", key, err)
	}
	return sshkey.AuthorizedKey(secret)
}

// resolveFiles reads any contentFrom references and returns the files to write.
func (r *CargoshipConfigReconciler) resolveFiles(ctx context.Context, config *bootstrapv1.CargoshipConfig) ([]cloudinit.File, error) {
	out := make([]cloudinit.File, 0, len(config.Spec.Files))
	for _, f := range config.Spec.Files {
		content := f.Content
		if f.ContentFrom != nil {
			secret := &corev1.Secret{}
			key := types.NamespacedName{
				Namespace: config.Namespace,
				Name:      f.ContentFrom.Secret.Name,
			}
			if err := r.Get(ctx, key, secret); err != nil {
				return nil, fmt.Errorf("unable to read %s for %s: %w", key, f.Path, err)
			}
			value, ok := secret.Data[f.ContentFrom.Secret.Key]
			if !ok {
				return nil, fmt.Errorf("secret %s has no key %q, needed for %s", key, f.ContentFrom.Secret.Key, f.Path)
			}
			content = string(value)
		}
		out = append(out, cloudinit.File{
			Path:        f.Path,
			Owner:       f.Owner,
			Permissions: f.Permissions,
			Content:     content,
		})
	}
	return out, nil
}

// writeSecret stores the bootstrap data in the shape the Machine controller reads.
func (r *CargoshipConfigReconciler) writeSecret(
	ctx context.Context,
	config *bootstrapv1.CargoshipConfig,
	machine *clusterv1.Machine,
	data []byte,
) (string, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: machine.Spec.ClusterName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: bootstrapv1.GroupVersion.String(),
					Kind:       "CargoshipConfig",
					Name:       config.Name,
					UID:        config.UID,
					Controller: ptr.To(true),
				},
			},
		},
		// The key name is fixed by the contract, not chosen here.
		Data: map[string][]byte{"value": data},
		Type: clusterv1.ClusterSecretType,
	}

	if err := r.Create(ctx, secret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("unable to create the bootstrap secret: %w", err)
		}
		// Left over from a previous attempt that wrote the secret and then failed to record it
		// in status. The data is a pure function of the spec, so updating it is safe.
		if err := r.Update(ctx, secret); err != nil {
			return "", fmt.Errorf("unable to update the existing bootstrap secret: %w", err)
		}
	}
	return secret.Name, nil
}

// SetupWithManager wires the reconciler up.
func (r *CargoshipConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1.CargoshipConfig{}).
		// A config cannot render until its owning Machine exists, and nothing else would wake it
		// when one appears.
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(configsForMachine),
		).
		Named("cargoshipconfig").
		Complete(r)
}

// configsForMachine maps a Machine to the CargoshipConfig it references.
func configsForMachine(_ context.Context, obj client.Object) []ctrl.Request {
	machine, ok := obj.(*clusterv1.Machine)
	if !ok {
		return nil
	}
	ref := machine.Spec.Bootstrap.ConfigRef
	if ref.Kind != "CargoshipConfig" {
		return nil
	}
	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: machine.Namespace,
				Name:      ref.Name,
			},
		},
	}
}
