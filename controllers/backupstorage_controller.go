package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupStorageReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupStorageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("backupStorage", req.NamespacedName.String())

	var storage dpv1alpha1.BackupStorage
	if err := r.Get(ctx, req.NamespacedName, &storage); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	original := storage.DeepCopy()
	storage.Status.ObservedGeneration = storage.Generation
	storage.Status.LastValidatedTime = nowTime()

	if err := storage.Spec.ValidateBasic(); err != nil {
		storage.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		markCondition(&storage.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", err.Error(), storage.Generation)
	} else {
		storage.Status.Phase = dpv1alpha1.ResourcePhaseReady
		markCondition(&storage.Status.Conditions, "Ready", metav1.ConditionTrue, "Validated", "backup storage specification is valid", storage.Generation)
	}

	if err := r.Status().Patch(ctx, &storage, client.MergeFrom(original)); err != nil {
		logger.Error(err, "unable to patch BackupStorage status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupStorageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupStorage{}).
		Complete(r)
}
