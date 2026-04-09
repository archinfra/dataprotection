package controllers

import (
	"context"
	"sort"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

type BackupPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var policy dpv1alpha1.BackupPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := policy.DeepCopy()
	policy.Status.ObservedGeneration = policy.Generation

	if err := policy.Spec.ValidateBasic(); err != nil {
		policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		policy.Status.Message = err.Error()
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "InvalidSpec", policy.Status.Message, policy.Generation)
		if err := r.Status().Patch(ctx, &policy, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	source, addon, storages, retention, err := resolvePolicyDependencies(ctx, r.Client, &policy)
	if err != nil {
		policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
		policy.Status.Message = err.Error()
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "DependencyError", policy.Status.Message, policy.Generation)
		if err := r.Status().Patch(ctx, &policy, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}
	for _, ref := range policy.Spec.NotificationRefs {
		if _, err := getNotificationEndpoint(ctx, r.Client, policy.Namespace, ref.Name); err != nil {
			policy.Status.Phase = dpv1alpha1.ResourcePhaseFailed
			policy.Status.Message = err.Error()
			markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "NotificationEndpointError", policy.Status.Message, policy.Generation)
			if err := r.Status().Patch(ctx, &policy, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return requeueSoon(), nil
		}
	}

	desired := map[string]struct{}{}
	cronJobNames := make([]string, 0, len(storages))
	for _, storage := range storages {
		cronJob, err := buildBackupCronJob(&policy, source, addon, storage, retention)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := controllerutil.SetControllerReference(&policy, cronJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		desired[cronJob.Name] = struct{}{}
		cronJobNames = append(cronJobNames, cronJob.Name)
		current := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: cronJob.Name, Namespace: cronJob.Namespace}}
		template := cronJob.DeepCopy()
		if err := createOrUpdateWithRetry(ctx, r.Client, current, func() error {
			current.Labels = copyStringMap(template.Labels)
			current.Annotations = copyStringMap(template.Annotations)
			current.Spec = template.Spec
			return nil
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	var existing batchv1.CronJobList
	if err := r.List(ctx, &existing,
		client.InNamespace(policy.Namespace),
		client.MatchingLabels{
			managedByLabel:     managedByValue,
			executionKindLabel: dpv1alpha1.BuildLabelValue("BackupPolicy"),
			policyNameLabel:    dpv1alpha1.BuildLabelValue(policy.Name),
			operationLabel:     dpv1alpha1.BuildLabelValue("backup"),
		},
	); err != nil {
		return ctrl.Result{}, err
	}
	for i := range existing.Items {
		if _, ok := desired[existing.Items[i].Name]; ok {
			continue
		}
		if err := deleteInBackground(ctx, r.Client, &existing.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	sort.Strings(cronJobNames)
	policy.Status.CronJobNames = cronJobNames
	if source.Spec.Paused || policy.Spec.Suspend || policy.Spec.Schedule.Suspend {
		policy.Status.Phase = dpv1alpha1.ResourcePhasePaused
		policy.Status.Message = "backup policy is suspended"
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionFalse, "Paused", policy.Status.Message, policy.Generation)
	} else {
		policy.Status.Phase = dpv1alpha1.ResourcePhaseConfigured
		policy.Status.Message = "backup policy is configured"
		markCondition(&policy.Status.Conditions, "Ready", metav1.ConditionTrue, "Configured", policy.Status.Message, policy.Generation)
	}
	policy.Status.LastScheduleTime = latestCronScheduleTime(existing.Items, cronJobNames)
	if err := r.Status().Patch(ctx, &policy, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func latestCronScheduleTime(items []batchv1.CronJob, names []string) *metav1.Time {
	allowed := map[string]struct{}{}
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	var latest *metav1.Time
	for i := range items {
		if _, ok := allowed[items[i].Name]; !ok {
			continue
		}
		if items[i].Status.LastScheduleTime == nil {
			continue
		}
		if latest == nil || items[i].Status.LastScheduleTime.After(latest.Time) {
			ts := *items[i].Status.LastScheduleTime
			latest = &ts
		}
	}
	return latest
}

func (r *BackupPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.BackupPolicy{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}
