package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

func triggerServiceAccountName(policyName string) string {
	return dpv1alpha1.BuildJobName(policyName, "trigger")
}

func triggerRoleName(policyName string) string {
	return dpv1alpha1.BuildJobName(policyName, "trigger-role")
}

func triggerRoleBindingName(policyName string) string {
	return dpv1alpha1.BuildJobName(policyName, "trigger-binding")
}

func buildTriggerPolicyRules(policyName string) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups:     []string{"dataprotection.archinfra.io"},
			Resources:     []string{"backuppolicies"},
			ResourceNames: []string{policyName},
			Verbs:         []string{"get"},
		},
		{
			APIGroups: []string{"dataprotection.archinfra.io"},
			Resources: []string{"backupruns"},
			Verbs:     []string{"create", "list", "delete"},
		},
	}
}

// ensureTriggerAccess provisions the namespace-scoped identity used by a
// BackupPolicy's CronJobs. Each policy gets its own least-privilege service
// account so scheduled execution does not depend on the operator namespace.
func ensureTriggerAccess(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	policy *dpv1alpha1.BackupPolicy,
) (string, error) {
	serviceAccountName := triggerServiceAccountName(policy.Name)
	labels := managedResourceLabels("BackupPolicy", policy.Name, "scheduled-trigger-auth", policy.Spec.SourceRef.Name, "")

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: policy.Namespace,
		},
	}
	if err := createOrUpdateWithRetry(ctx, c, serviceAccount, func() error {
		serviceAccount.Labels = mergeStringMaps(serviceAccount.Labels, labels)
		return controllerutil.SetControllerReference(policy, serviceAccount, scheme)
	}); err != nil {
		return "", err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      triggerRoleName(policy.Name),
			Namespace: policy.Namespace,
		},
	}
	if err := createOrUpdateWithRetry(ctx, c, role, func() error {
		role.Labels = mergeStringMaps(role.Labels, labels)
		role.Rules = buildTriggerPolicyRules(policy.Name)
		return controllerutil.SetControllerReference(policy, role, scheme)
	}); err != nil {
		return "", err
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      triggerRoleBindingName(policy.Name),
			Namespace: policy.Namespace,
		},
	}
	if err := createOrUpdateWithRetry(ctx, c, roleBinding, func() error {
		roleBinding.Labels = mergeStringMaps(roleBinding.Labels, labels)
		roleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     role.Name,
		}
		roleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount.Name,
				Namespace: policy.Namespace,
			},
		}
		return controllerutil.SetControllerReference(policy, roleBinding, scheme)
	}); err != nil {
		return "", err
	}

	return serviceAccountName, nil
}
