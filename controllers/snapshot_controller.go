package controllers

import (
	"context"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

const snapshotStorageCleanupFinalizer = "dataprotection.archinfra.io/storage-cleanup"

type SnapshotReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *SnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var snapshot dpv1alpha1.Snapshot
	if err := r.Get(ctx, req.NamespacedName, &snapshot); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if snapshot.DeletionTimestamp.IsZero() {
		if snapshot.Status.ArtifactReady && !controllerutil.ContainsFinalizer(&snapshot, snapshotStorageCleanupFinalizer) {
			base := snapshot.DeepCopy()
			controllerutil.AddFinalizer(&snapshot, snapshotStorageCleanupFinalizer)
			if err := r.Patch(ctx, &snapshot, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&snapshot, snapshotStorageCleanupFinalizer) {
		return ctrl.Result{}, nil
	}

	if !snapshot.Status.ArtifactReady || trimString(snapshot.Spec.StorageRef.Name) == "" || trimString(snapshot.Spec.BackendPath) == "" || trimString(snapshot.Spec.Snapshot) == "" {
		base := snapshot.DeepCopy()
		controllerutil.RemoveFinalizer(&snapshot, snapshotStorageCleanupFinalizer)
		if err := r.Patch(ctx, &snapshot, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	storage, err := getBackupStorage(ctx, r.Client, snapshot.Namespace, snapshot.Spec.StorageRef.Name)
	if err != nil {
		return requeueSoon(), err
	}

	cleanupName := dpv1alpha1.BuildJobName(snapshot.Name, "cleanup")
	var cleanupJob batchv1.Job
	if err := r.Get(ctx, client.ObjectKey{Namespace: snapshot.Namespace, Name: cleanupName}, &cleanupJob); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		rendered := buildSnapshotCleanupJob(&snapshot, storage)
		if err := controllerutil.SetControllerReference(&snapshot, rendered, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, rendered); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		return requeueSoon(), nil
	}

	phase, message, done := jobTerminalState(&cleanupJob)
	if !done {
		return requeueSoon(), nil
	}
	if phase != dpv1alpha1.ResourcePhaseSucceeded {
		return requeueSoon(), fmt.Errorf("snapshot cleanup job %s failed: %s", cleanupJob.Name, message)
	}

	base := snapshot.DeepCopy()
	controllerutil.RemoveFinalizer(&snapshot, snapshotStorageCleanupFinalizer)
	if err := r.Patch(ctx, &snapshot, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *SnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpv1alpha1.Snapshot{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func buildSnapshotCleanupJob(snapshot *dpv1alpha1.Snapshot, storage *dpv1alpha1.BackupStorage) *batchv1.Job {
	labels := managedResourceLabels("Snapshot", snapshot.Name, "cleanup", snapshot.Spec.SourceRef.Name, storage.Name)
	annotations := map[string]string{
		seriesAnnotation:      snapshot.Spec.Series,
		backendPathAnnotation: snapshot.Spec.BackendPath,
	}
	env := append(buildStorageEnv(storage),
		corev1.EnvVar{Name: "DP_BACKEND_PATH", Value: snapshot.Spec.BackendPath},
		corev1.EnvVar{Name: "DP_SNAPSHOT", Value: snapshot.Spec.Snapshot},
	)

	image := defaultUtilityImage()
	script := buildNFSCleanupScript()
	volumes := []corev1.Volume{}
	if storage.Spec.Type == dpv1alpha1.StorageTypeNFS {
		volumes = append(volumes, corev1.Volume{
			Name: "backend-storage",
			VolumeSource: corev1.VolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Server: storage.Spec.NFS.Server,
					Path:   storage.Spec.NFS.Path,
				},
			},
		})
	}
	if storage.Spec.Type == dpv1alpha1.StorageTypeMinIO {
		image = defaultMinIOHelperImage()
		script = buildMinIOCleanupScript()
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dpv1alpha1.BuildJobName(snapshot.Name, "cleanup"),
			Namespace:   snapshot.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: singleExecutionJobSpec(batchv1.JobSpec{
			ActiveDeadlineSeconds:   defaultJobActiveDeadlineSeconds(),
			BackoffLimit:            defaultJobBackoffLimit(),
			TTLSecondsAfterFinished: defaultJobTTLSeconds(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      copyStringMap(labels),
					Annotations: copyStringMap(annotations),
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes:       volumes,
					Containers: []corev1.Container{{
						Name:            "artifact-cleanup",
						Image:           image,
						ImagePullPolicy: defaultImagePullPolicy(image),
						Command:         []string{"/bin/sh", "-ceu"},
						Args:            []string{script},
						Env:             env,
						VolumeMounts:    storageVolumeMounts(storage),
					}},
				},
			},
		}),
	}
}

func buildNFSCleanupScript() string {
	return strings.Join([]string{
		"set -eu",
		"snapshot=\"${DP_SNAPSHOT}\"",
		"base=\"/backend/${DP_BACKEND_PATH}\"",
		"mkdir -p \"${base}/snapshots\"",
		"rm -f \"${base}/snapshots/${snapshot}.tgz\" \"${base}/snapshots/${snapshot}.tgz.sha256\" \"${base}/snapshots/${snapshot}.metadata.json\"",
		"latest_source=\"$(find \"${base}/snapshots\" -maxdepth 1 -type f -name '*.metadata.json' | sed 's#.*/##' | grep -v \"^${snapshot}\\.metadata\\.json$\" | sort -r | head -n 1 || true)\"",
		"if [ -n \"${latest_source}\" ]; then",
		"  cp \"${base}/snapshots/${latest_source}\" \"${base}/latest.json\"",
		"else",
		"  rm -f \"${base}/latest.json\"",
		"fi",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "snapshot artifacts removed from nfs"}}) + "' > /dev/termination-log",
	}, "\n")
}

func buildMinIOCleanupScript() string {
	return strings.Join([]string{
		"set -eu",
		"mc_cmd() {",
		"  if [ \"${DP_MINIO_INSECURE:-false}\" = \"true\" ]; then",
		"    mc --insecure \"$@\"",
		"  else",
		"    mc \"$@\"",
		"  fi",
		"}",
		"snapshot=\"${DP_SNAPSHOT}\"",
		"remote_base=\"backup/${DP_MINIO_BUCKET}\"",
		"if [ -n \"${DP_MINIO_PREFIX:-}\" ]; then remote_base=\"${remote_base}/${DP_MINIO_PREFIX}\"; fi",
		"remote_base=\"${remote_base}/${DP_BACKEND_PATH}\"",
		"mc_cmd alias set backup \"${DP_MINIO_ENDPOINT}\" \"${DP_MINIO_ACCESS_KEY}\" \"${DP_MINIO_SECRET_KEY}\" >/dev/null",
		"if ! mc_cmd stat \"backup/${DP_MINIO_BUCKET}\" >/dev/null 2>&1; then",
		"  printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "minio bucket already missing; cleanup skipped"}}) + "' > /dev/termination-log",
		"  exit 0",
		"fi",
		"mc_cmd rm --force \"${remote_base}/snapshots/${snapshot}.tgz\" >/dev/null || true",
		"mc_cmd rm --force \"${remote_base}/snapshots/${snapshot}.tgz.sha256\" >/dev/null || true",
		"mc_cmd rm --force \"${remote_base}/snapshots/${snapshot}.metadata.json\" >/dev/null || true",
		"latest_source=\"$(mc_cmd ls \"${remote_base}/snapshots\" 2>/dev/null | awk '{print $NF}' | sed 's#/$##' | grep '\\.metadata\\.json$' | grep -v \"^${snapshot}\\.metadata\\.json$\" | sort -r | head -n 1 || true)\"",
		"if [ -n \"${latest_source}\" ]; then",
		"  mc_cmd cp \"${remote_base}/snapshots/${latest_source}\" \"${remote_base}/latest.json\" >/dev/null",
		"else",
		"  mc_cmd rm --force \"${remote_base}/latest.json\" >/dev/null || true",
		"fi",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "snapshot artifacts removed from minio"}}) + "' > /dev/termination-log",
	}, "\n")
}
