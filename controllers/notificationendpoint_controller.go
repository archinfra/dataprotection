package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type NotificationEndpointReconciler struct {
	client.Client
}

func (r *NotificationEndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var endpoint dpv1alpha1.NotificationEndpoint
	if err := r.Get(ctx, req.NamespacedName, &endpoint); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := endpoint.DeepCopy()
	endpoint.Status.ObservedGeneration = endpoint.Generation
	if err := endpoint.Spec.ValidateBasic(); err != nil {
		endpoint.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		endpoint.Status.Message = err.Error()
		markCondition(&endpoint.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", endpoint.Status.Message, endpoint.Generation)
	} else {
		endpoint.Status.Phase = dpv1alpha1.ResourcePhaseConfigured
		endpoint.Status.Message = "notification endpoint is configured"
		markCondition(&endpoint.Status.Conditions, "Ready", metav1.ConditionTrue, "Configured", endpoint.Status.Message, endpoint.Generation)
	}

	if err := r.Status().Patch(ctx, &endpoint, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *NotificationEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.NotificationEndpoint{}).
		Complete(r)
}
