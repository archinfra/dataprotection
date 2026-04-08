package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

// runTriggerBackupRun is executed by the schedule CronJob. The CronJob does not
// create data Jobs directly; instead it creates a BackupRun CR so execution,
// status and snapshots stay inside the operator's own API model.
func runTriggerBackupRun(args []string) error {
	fs := flag.NewFlagSet("trigger-backup-run", flag.ContinueOnError)
	var namespace string
	var policyName string
	var storageName string

	fs.StringVar(&namespace, "namespace", "", "Namespace of the BackupPolicy and BackupRun")
	fs.StringVar(&policyName, "policy", "", "Name of the BackupPolicy to trigger")
	fs.StringVar(&storageName, "storage", "", "Name of the BackupStorage to trigger")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(policyName) == "" || strings.TrimSpace(storageName) == "" {
		return fmt.Errorf("--namespace, --policy and --storage are required")
	}

	cfg := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	ctx := context.Background()
	policy := &dpv1alpha1.BackupPolicy{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: policyName}, policy); err != nil {
		return err
	}
	createRun, err := enforceTriggeredRunConcurrency(ctx, k8sClient, policy, storageName)
	if err != nil {
		return err
	}
	if !createRun {
		ctrl.Log.WithName("trigger").Info("skip scheduled BackupRun because another run is still active",
			"namespace", namespace,
			"policy", policyName,
			"storage", storageName,
			"concurrencyPolicy", policy.Spec.EffectiveConcurrencyPolicy(),
		)
		return nil
	}

	run := &dpv1alpha1.BackupRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: dpv1alpha1.BuildCronJobName(policy.Name, "scheduled") + "-",
			Namespace:    namespace,
			Labels: map[string]string{
				"dataprotection.archinfra.io/triggered-by": "cronjob",
				"dataprotection.archinfra.io/policy-name":  dpv1alpha1.BuildLabelValue(policy.Name),
				"dataprotection.archinfra.io/storage-name": dpv1alpha1.BuildLabelValue(storageName),
			},
		},
		Spec: dpv1alpha1.BackupRunSpec{
			PolicyRef:   &corev1.LocalObjectReference{Name: policy.Name},
			SourceRef:   policy.Spec.SourceRef,
			StorageRefs: []corev1.LocalObjectReference{{Name: storageName}},
			Reason:      fmt.Sprintf("scheduled by policy/%s", policy.Name),
		},
	}
	if err := controllerutil.SetControllerReference(policy, run, scheme); err != nil {
		return err
	}

	return k8sClient.Create(ctx, run)
}

func enforceTriggeredRunConcurrency(
	ctx context.Context,
	k8sClient client.Client,
	policy *dpv1alpha1.BackupPolicy,
	storageName string,
) (bool, error) {
	activeRuns, err := listActiveScheduledRuns(ctx, k8sClient, policy.Namespace, policy.Name, storageName)
	if err != nil {
		return false, err
	}
	switch policy.Spec.EffectiveConcurrencyPolicy() {
	case batchv1.AllowConcurrent:
		return true, nil
	case batchv1.ReplaceConcurrent:
		for i := range activeRuns {
			if err := k8sClient.Delete(ctx, &activeRuns[i]); err != nil {
				return false, err
			}
		}
		return true, nil
	case batchv1.ForbidConcurrent:
		fallthrough
	default:
		return len(activeRuns) == 0, nil
	}
}

func listActiveScheduledRuns(
	ctx context.Context,
	k8sClient client.Client,
	namespace, policyName, storageName string,
) ([]dpv1alpha1.BackupRun, error) {
	var runList dpv1alpha1.BackupRunList
	if err := k8sClient.List(ctx, &runList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"dataprotection.archinfra.io/triggered-by": "cronjob",
			"dataprotection.archinfra.io/policy-name":  dpv1alpha1.BuildLabelValue(policyName),
			"dataprotection.archinfra.io/storage-name": dpv1alpha1.BuildLabelValue(storageName),
		},
	); err != nil {
		return nil, err
	}

	activeRuns := make([]dpv1alpha1.BackupRun, 0, len(runList.Items))
	for i := range runList.Items {
		run := runList.Items[i]
		if run.DeletionTimestamp != nil {
			continue
		}
		if isTerminalBackupRun(run.Status.Phase) {
			continue
		}
		activeRuns = append(activeRuns, run)
	}
	return activeRuns, nil
}

func isTerminalBackupRun(phase dpv1alpha1.ResourcePhase) bool {
	switch phase {
	case dpv1alpha1.ResourcePhaseSucceeded, dpv1alpha1.ResourcePhaseFailed:
		return true
	default:
		return false
	}
}
