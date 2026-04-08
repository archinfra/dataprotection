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

type BackupSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("backupSource", req.NamespacedName.String())

	var source dpv1alpha1.BackupSource
	if err := r.Get(ctx, req.NamespacedName, &source); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	original := source.DeepCopy()
	source.Status.ObservedGeneration = source.Generation
	source.Status.LastValidatedTime = nowTime()

	if err := source.Spec.ValidateBasic(); err != nil {
		source.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		source.Status.Message = err.Error()
		markCondition(&source.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", err.Error(), source.Generation)
	} else if source.Spec.Paused {
		source.Status.Phase = dpv1alpha1.ResourcePhasePaused
		source.Status.Message = phaseMessage(source.Status.Phase)
		markCondition(&source.Status.Conditions, "Ready", metav1.ConditionFalse, "Paused", phaseMessage(source.Status.Phase), source.Generation)
	} else {
		source.Status.Phase = dpv1alpha1.ResourcePhaseReady
		source.Status.Message = "backup source specification is valid"
		markCondition(&source.Status.Conditions, "Ready", metav1.ConditionTrue, "Validated", "backup source specification is valid", source.Generation)
	}

	if err := r.Status().Patch(ctx, &source, client.MergeFrom(original)); err != nil {
		logger.Error(err, "unable to patch BackupSource status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupSource{}).
		Complete(r)
}
