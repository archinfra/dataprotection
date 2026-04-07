package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

	return k8sClient.Create(ctx, run)
}
