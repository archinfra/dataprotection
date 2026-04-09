package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type RetentionPolicyReconciler struct {
	client.Client
}

func (r *RetentionPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var policy dpv1alpha1.RetentionPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := policy.DeepCopy()
	policy.Status.ObservedGeneration = policy.Generation
	if err := policy.Spec.ValidateBasic(); err != nil {
		policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		policy.Status.Message = err.Error()
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", policy.Status.Message, policy.Generation)
	} else {
		policy.Status.Phase = dpv1alpha1.ResourcePhaseConfigured
		policy.Status.Message = "retention policy is configured"
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionTrue, "Configured", policy.Status.Message, policy.Generation)
	}

	if err := r.Status().Patch(ctx, &policy, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *RetentionPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.RetentionPolicy{}).
		Complete(r)
}
