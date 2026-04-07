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

type RetentionPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *RetentionPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("retentionPolicy", req.NamespacedName.String())

	var retentionPolicy dpv1alpha1.RetentionPolicy
	if err := r.Get(ctx, req.NamespacedName, &retentionPolicy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	original := retentionPolicy.DeepCopy()
	retentionPolicy.Status.ObservedGeneration = retentionPolicy.Generation

	if err := retentionPolicy.Spec.ValidateBasic(); err != nil {
		retentionPolicy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		markCondition(&retentionPolicy.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", err.Error(), retentionPolicy.Generation)
	} else {
		retentionPolicy.Status.Phase = dpv1alpha1.ResourcePhaseReady
		markCondition(&retentionPolicy.Status.Conditions, "Ready", metav1.ConditionTrue, "Validated", "retention policy specification is valid", retentionPolicy.Generation)
	}

	if err := r.Status().Patch(ctx, &retentionPolicy, client.MergeFrom(original)); err != nil {
		logger.Error(err, "unable to patch RetentionPolicy status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *RetentionPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.RetentionPolicy{}).
		Complete(r)
}
