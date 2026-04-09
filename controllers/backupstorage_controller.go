package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupStorageReconciler struct {
	client.Client
}

func (r *BackupStorageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var storage dpv1alpha1.BackupStorage
	if err := r.Get(ctx, req.NamespacedName, &storage); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := storage.DeepCopy()
	storage.Status.ObservedGeneration = storage.Generation
	if storage.Status.LastProbeResult == "" {
		storage.Status.LastProbeResult = dpv1alpha1.ProbeResultUnknown
	}

	if err := storage.Spec.ValidateBasic(); err != nil {
		storage.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		storage.Status.Message = err.Error()
		markCondition(&storage.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", storage.Status.Message, storage.Generation)
	} else {
		storage.Status.Phase = dpv1alpha1.ResourcePhaseConfigured
		storage.Status.Message = "storage backend is configured; connectivity is checked before each execution"
		markCondition(&storage.Status.Conditions, "Ready", metav1.ConditionTrue, "Configured", storage.Status.Message, storage.Generation)
	}

	if err := r.Status().Patch(ctx, &storage, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupStorageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupStorage{}).
		Complete(r)
}
