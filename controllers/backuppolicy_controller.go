package controllers

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("backupPolicy", req.NamespacedName.String())

	var policy dpv1alpha1.BackupPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	original := policy.DeepCopy()
	policy.Status.ObservedGeneration = policy.Generation
	policy.Status.CronJobNames = dpv1alpha1.PredictCronJobNames(policy.Name, policy.Spec.RepositoryRefs)

	patchStatus := func(result ctrl.Result, reconcileErr error) (ctrl.Result, error) {
		if err := r.Status().Patch(ctx, &policy, client.MergeFrom(original)); err != nil {
			logger.Error(err, "unable to patch BackupPolicy status")
			return ctrl.Result{}, err
		}
		return result, reconcileErr
	}

	if err := policy.Spec.ValidateBasic(); err != nil {
		if cleanupErr := r.cleanupOwnedCronJobs(ctx, &policy, nil); cleanupErr != nil {
			return ctrl.Result{}, cleanupErr
		}
		policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		policy.Status.LastScheduleTime = nil
		policy.Status.NextScheduleTime = nil
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", err.Error(), policy.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	source, err := getBackupSource(ctx, r.Client, policy.Namespace, policy.Spec.SourceRef.Name)
	if err != nil {
		if cleanupErr := r.cleanupOwnedCronJobs(ctx, &policy, nil); cleanupErr != nil {
			return ctrl.Result{}, cleanupErr
		}
		policy.Status.Phase = dpv1alpha1.ResourcePhasePending
		policy.Status.LastScheduleTime = nil
		policy.Status.NextScheduleTime = nil
		reason := "DependencyNotReady"
		if !isDependencyMissing(err) {
			reason = "DependencyLookupFailed"
		}
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, reason, fmt.Sprintf("unable to resolve BackupSource %q: %v", policy.Spec.SourceRef.Name, err), policy.Generation)
		return patchStatus(requeueSoon(), nil)
	}
	if err := source.Spec.ValidateBasic(); err != nil {
		if cleanupErr := r.cleanupOwnedCronJobs(ctx, &policy, nil); cleanupErr != nil {
			return ctrl.Result{}, cleanupErr
		}
		policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		policy.Status.LastScheduleTime = nil
		policy.Status.NextScheduleTime = nil
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSource", fmt.Sprintf("referenced BackupSource %q is invalid: %v", source.Name, err), policy.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	repositories, err := resolveRepositories(ctx, r.Client, policy.Namespace, policy.Spec.RepositoryRefs)
	if err != nil {
		if cleanupErr := r.cleanupOwnedCronJobs(ctx, &policy, nil); cleanupErr != nil {
			return ctrl.Result{}, cleanupErr
		}
		policy.Status.Phase = dpv1alpha1.ResourcePhasePending
		policy.Status.LastScheduleTime = nil
		policy.Status.NextScheduleTime = nil
		reason := "DependencyNotReady"
		if isPermanentDependencyError(err) {
			policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			reason = "InvalidRepository"
		} else if !isDependencyMissing(err) {
			reason = "DependencyLookupFailed"
		}
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, reason, fmt.Sprintf("unable to resolve BackupRepository references: %v", err), policy.Generation)
		if policy.Status.Phase == dpv1alpha1.ResourcePhaseFailed {
			return patchStatus(ctrl.Result{}, nil)
		}
		return patchStatus(requeueSoon(), nil)
	}
	for _, repository := range repositories {
		if err := repository.Spec.ValidateBasic(); err != nil {
			if cleanupErr := r.cleanupOwnedCronJobs(ctx, &policy, nil); cleanupErr != nil {
				return ctrl.Result{}, cleanupErr
			}
			policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			policy.Status.LastScheduleTime = nil
			policy.Status.NextScheduleTime = nil
			markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidRepository", fmt.Sprintf("referenced BackupRepository %q is invalid: %v", repository.Name, err), policy.Generation)
			return patchStatus(ctrl.Result{}, nil)
		}
	}

	keepLast, _, err := resolveKeepLastRetention(ctx, r.Client, policy.Namespace, policy.Spec.Retention, localRefName(policy.Spec.RetentionPolicyRef))
	if err != nil {
		if cleanupErr := r.cleanupOwnedCronJobs(ctx, &policy, nil); cleanupErr != nil {
			return ctrl.Result{}, cleanupErr
		}
		policy.Status.Phase = dpv1alpha1.ResourcePhasePending
		policy.Status.LastScheduleTime = nil
		policy.Status.NextScheduleTime = nil
		reason := "DependencyNotReady"
		if isPermanentDependencyError(err) {
			policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			reason = "InvalidRetentionPolicy"
		} else if !isDependencyMissing(err) {
			reason = "DependencyLookupFailed"
		}
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, reason, err.Error(), policy.Generation)
		if policy.Status.Phase == dpv1alpha1.ResourcePhaseFailed {
			return patchStatus(ctrl.Result{}, nil)
		}
		return patchStatus(requeueSoon(), nil)
	}

	desiredCronJobNames := make(map[string]struct{}, len(repositories))
	var latestLastScheduleTime *metav1.Time
	for i := range repositories {
		desired, err := buildBackupCronJob(&policy, source, &repositories[i], keepLast)
		if err != nil {
			policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			policy.Status.LastScheduleTime = nil
			policy.Status.NextScheduleTime = nil
			markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "RenderFailed", err.Error(), policy.Generation)
			return patchStatus(ctrl.Result{}, nil)
		}
		desiredCronJobNames[desired.Name] = struct{}{}

		current := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
			current.Labels = mergeStringMaps(current.Labels, desired.Labels)
			current.Annotations = mergeStringMaps(current.Annotations, desired.Annotations)
			current.Spec = desired.Spec
			return controllerutil.SetControllerReference(&policy, current, r.Scheme)
		}); err != nil {
			return ctrl.Result{}, err
		}

		if current.Status.LastScheduleTime != nil && (latestLastScheduleTime == nil || current.Status.LastScheduleTime.After(latestLastScheduleTime.Time)) {
			latestLastScheduleTime = current.Status.LastScheduleTime.DeepCopy()
		}
	}

	if err := r.cleanupOwnedCronJobs(ctx, &policy, desiredCronJobNames); err != nil {
		return ctrl.Result{}, err
	}

	policy.Status.LastScheduleTime = latestLastScheduleTime
	policy.Status.NextScheduleTime = nil

	suspended := policy.Spec.Suspend || policy.Spec.Schedule.Suspend
	if suspended {
		policy.Status.Phase = dpv1alpha1.ResourcePhasePaused
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "Suspended", fmt.Sprintf("managed %d suspended CronJob resource(s)", len(desiredCronJobNames)), policy.Generation)
		return patchStatus(ctrl.Result{}, nil)
	}

	policy.Status.Phase = dpv1alpha1.ResourcePhaseReady
	markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionTrue, "Reconciled", fmt.Sprintf("managed %d CronJob resource(s) across %d repository target(s)", len(desiredCronJobNames), len(repositories)), policy.Generation)
	return patchStatus(ctrl.Result{}, nil)
}

func (r *BackupPolicyReconciler) cleanupOwnedCronJobs(ctx context.Context, policy *dpv1alpha1.BackupPolicy, desired map[string]struct{}) error {
	var cronJobs batchv1.CronJobList
	if err := r.List(ctx, &cronJobs, client.InNamespace(policy.Namespace)); err != nil {
		return err
	}

	for i := range cronJobs.Items {
		cronJob := &cronJobs.Items[i]
		if !metav1.IsControlledBy(cronJob, policy) {
			continue
		}
		if _, keep := desired[cronJob.Name]; keep {
			continue
		}
		if err := r.Delete(ctx, cronJob); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *BackupPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupPolicy{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}
