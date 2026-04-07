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

type BackupRepositoryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("backupRepository", req.NamespacedName.String())

	var repository dpv1alpha1.BackupRepository
	if err := r.Get(ctx, req.NamespacedName, &repository); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	original := repository.DeepCopy()
	repository.Status.ObservedGeneration = repository.Generation
	repository.Status.LastValidatedTime = nowTime()

	if err := repository.Spec.ValidateBasic(); err != nil {
		repository.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		markCondition(&repository.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", err.Error(), repository.Generation)
	} else {
		repository.Status.Phase = dpv1alpha1.ResourcePhaseReady
		markCondition(&repository.Status.Conditions, "Ready", metav1.ConditionTrue, "Validated", "backup repository specification is valid", repository.Generation)
	}

	if err := r.Status().Patch(ctx, &repository, client.MergeFrom(original)); err != nil {
		logger.Error(err, "unable to patch BackupRepository status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupRepository{}).
		Complete(r)
}
