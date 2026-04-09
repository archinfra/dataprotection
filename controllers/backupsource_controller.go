package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupSourceReconciler struct {
	client.Client
}

func (r *BackupSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var source dpv1alpha1.BackupSource
	if err := r.Get(ctx, req.NamespacedName, &source); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := source.DeepCopy()
	source.Status.ObservedGeneration = source.Generation
	source.Status.LastValidatedTime = nowTime()

	if err := source.Spec.ValidateBasic(); err != nil {
		source.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		source.Status.Message = err.Error()
		markCondition(&source.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", source.Status.Message, source.Generation)
	} else if _, err := getBackupAddon(ctx, r.Client, source.Spec.AddonRef.Name); err != nil {
		source.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		source.Status.Message = err.Error()
		markCondition(&source.Status.Conditions, "Ready", metav1.ConditionFalse, "AddonNotFound", source.Status.Message, source.Generation)
	} else if source.Spec.Paused {
		source.Status.Phase = dpv1alpha1.ResourcePhasePaused
		source.Status.Message = "backup source is paused"
		markCondition(&source.Status.Conditions, "Ready", metav1.ConditionFalse, "Paused", source.Status.Message, source.Generation)
	} else {
		source.Status.Phase = dpv1alpha1.ResourcePhaseConfigured
		source.Status.Message = "backup source configuration is valid"
		markCondition(&source.Status.Conditions, "Ready", metav1.ConditionTrue, "Configured", source.Status.Message, source.Generation)
	}

	if err := r.Status().Patch(ctx, &source, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupSource{}).
		Complete(r)
}
