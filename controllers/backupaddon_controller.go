package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupAddonReconciler struct {
	client.Client
}

func (r *BackupAddonReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var addon dpv1alpha1.BackupAddon
	if err := r.Get(ctx, req.NamespacedName, &addon); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	base := addon.DeepCopy()
	addon.Status.ObservedGeneration = addon.Generation
	if err := addon.Spec.ValidateBasic(); err != nil {
		addon.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		addon.Status.Message = err.Error()
		markCondition(&addon.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", addon.Status.Message, addon.Generation)
	} else {
		addon.Status.Phase = dpv1alpha1.ResourcePhaseConfigured
		addon.Status.Message = "addon template is configured"
		markCondition(&addon.Status.Conditions, "Ready", metav1.ConditionTrue, "Configured", addon.Status.Message, addon.Generation)
	}
	if err := r.Status().Patch(ctx, &addon, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupAddonReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupAddon{}).
		Complete(r)
}
