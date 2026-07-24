package controller

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
	"github.com/Danpiel/backrest-operator/internal/filters"
	"github.com/Danpiel/backrest-operator/internal/metrics"
)

var volumeSnapshotGVK = schema.GroupVersionKind{
	Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot",
}

type PVCBackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *PVCBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var backup operatorv1alpha1.PVCBackup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !filters.ObjectAllowed(backup.Namespace, backup.Labels) {
		return ctrl.Result{}, nil
	}
	if backup.Status.Phase == "Succeeded" && backup.Spec.Schedule == "" {
		return ctrl.Result{}, nil
	}
	if backup.Status.Phase == "Uploading" || backup.Status.Phase == "Snapshotting" {
		// avoid double-run; rely on status
	}

	started := time.Now()
	var quiesceState map[string]int32
	defer func() {
		if !backup.Spec.Quiesce.LeaveDown && len(quiesceState) > 0 {
			_ = r.unquiesce(ctx, backup.Namespace, quiesceState)
		}
	}()

	pipeline := backup.Spec.Strategy.Pipeline
	if len(pipeline) == 0 {
		pipeline = []string{"csiSnapshot"}
	}

	repoNS := backup.Spec.RepositoryRef.Namespace
	if repoNS == "" {
		repoNS = backup.Namespace
	}
	var repo operatorv1alpha1.BackupRepository
	if err := r.Get(ctx, types.NamespacedName{Name: backup.Spec.RepositoryRef.Name, Namespace: repoNS}, &repo); err != nil {
		return r.fail(ctx, &backup, err)
	}

	needQuiesce := backup.Spec.Quiesce.Enabled
	for _, s := range pipeline {
		if s == "quiescedLive" {
			needQuiesce = true
		}
	}
	if needQuiesce {
		backup.Status.Phase = "Quiescing"
		_ = r.Status().Update(ctx, &backup)
		var err error
		quiesceState, err = r.quiesce(ctx, &backup)
		if err != nil {
			return r.fail(ctx, &backup, err)
		}
	}

	uploadPVC := backup.Spec.PVCName
	var snapName string
	for _, step := range pipeline {
		if step == "csiSnapshot" || step == "topolvmSnapshot" {
			if backup.Spec.VolumeSnapshotClassName == "" {
				return r.fail(ctx, &backup, fmt.Errorf("volumeSnapshotClassName required"))
			}
			backup.Status.Phase = "Snapshotting"
			_ = r.Status().Update(ctx, &backup)
			snapName = fmt.Sprintf("%s-%d", backup.Name, started.Unix())
			if err := r.createSnapshot(ctx, &backup, snapName); err != nil {
				return r.fail(ctx, &backup, err)
			}
			clone := fmt.Sprintf("%s-clone-%d", backup.Name, started.Unix())
			if err := r.cloneFromSnapshot(ctx, &backup, snapName, clone); err != nil {
				return r.fail(ctx, &backup, err)
			}
			uploadPVC = clone
		}
	}

	backup.Status.Phase = "Uploading"
	_ = r.Status().Update(ctx, &backup)
	jobName := fmt.Sprintf("pvcbackup-%s-%d", backup.Name, started.Unix())
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}
	if err := r.createResticBackupJob(ctx, &backup, &repo, jobName, uploadPVC); err != nil {
		return r.fail(ctx, &backup, err)
	}
	// Non-blocking: mark pending job; a follow-up reconcile can check Job status.
	// For simplicity wait briefly via requeue.
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: backup.Namespace}, &job); err == nil {
		if job.Status.Succeeded > 0 {
			dur := time.Since(started).Seconds()
			metrics.BackupTotal.WithLabelValues(backup.Namespace, backup.Name, "success").Inc()
			metrics.BackupDuration.WithLabelValues(backup.Namespace, backup.Name).Observe(dur)
			metrics.BackupLastSuccess.WithLabelValues(backup.Namespace, backup.Name).Set(float64(time.Now().Unix()))
			backup.Status.Phase = "Succeeded"
			backup.Status.LastBackupTime = time.Now().UTC().Format(time.RFC3339)
			backup.Status.LastSnapshotName = snapName
			backup.Status.LastJobName = jobName
			backup.Status.LastDurationSeconds = int64(dur)
			if snapName != "" {
				del := true
				if backup.Spec.Retention.DeleteVolumeSnapshotAfterUpload != nil {
					del = *backup.Spec.Retention.DeleteVolumeSnapshotAfterUpload
				}
				if del {
					_ = r.deleteSnapshot(ctx, backup.Namespace, snapName)
				}
			}
			return ctrl.Result{}, r.Status().Update(ctx, &backup)
		}
		if job.Status.Failed > 0 {
			metrics.BackupTotal.WithLabelValues(backup.Namespace, backup.Name, "failure").Inc()
			return r.fail(ctx, &backup, fmt.Errorf("restic job failed"))
		}
	}
	logger.Info("waiting for restic job", "job", jobName)
	backup.Status.LastJobName = jobName
	_ = r.Status().Update(ctx, &backup)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *PVCBackupReconciler) fail(ctx context.Context, b *operatorv1alpha1.PVCBackup, err error) (ctrl.Result, error) {
	metrics.ReconcileErrors.WithLabelValues("PVCBackup").Inc()
	metrics.BackupTotal.WithLabelValues(b.Namespace, b.Name, "failure").Inc()
	b.Status.Phase = "Failed"
	b.Status.Conditions = []operatorv1alpha1.Condition{{Type: "Failed", Status: "True", Message: err.Error()}}
	_ = r.Status().Update(ctx, b)
	return ctrl.Result{}, err
}

func (r *PVCBackupReconciler) quiesce(ctx context.Context, b *operatorv1alpha1.PVCBackup) (map[string]int32, error) {
	state := map[string]int32{}
	for _, t := range b.Spec.Quiesce.Targets {
		ns := t.Namespace
		if ns == "" {
			ns = b.Namespace
		}
		prev, err := r.scaleWorkload(ctx, t.Kind, ns, t.Name, 0)
		if err != nil {
			return state, err
		}
		state[t.Kind+"/"+ns+"/"+t.Name] = prev
	}
	return state, nil
}

func (r *PVCBackupReconciler) unquiesce(ctx context.Context, defaultNS string, state map[string]int32) error {
	for key, replicas := range state {
		var kind, ns, name string
		fmt.Sscanf(key, "%s/%s/%s", &kind, &ns, &name)
		// key format Kind/ns/name — Sscanf with %s stops at /
		parts := split3(key)
		if len(parts) != 3 {
			continue
		}
		_, _ = r.scaleWorkload(ctx, parts[0], parts[1], parts[2], replicas)
	}
	return nil
}

func split3(s string) []string {
	out := []string{}
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	out = append(out, cur)
	return out
}

func (r *PVCBackupReconciler) scaleWorkload(ctx context.Context, kind, ns, name string, replicas int32) (int32, error) {
	switch kind {
	case "StatefulSet":
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"})
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, u); err != nil {
			return 0, err
		}
		prev, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
		if err := unstructured.SetNestedField(u.Object, int64(replicas), "spec", "replicas"); err != nil {
			return 0, err
		}
		return int32(prev), r.Update(ctx, u)
	case "Deployment":
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, u); err != nil {
			return 0, err
		}
		prev, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
		if err := unstructured.SetNestedField(u.Object, int64(replicas), "spec", "replicas"); err != nil {
			return 0, err
		}
		return int32(prev), r.Update(ctx, u)
	default:
		return 0, fmt.Errorf("unsupported quiesce kind %s", kind)
	}
}

func (r *PVCBackupReconciler) createSnapshot(ctx context.Context, b *operatorv1alpha1.PVCBackup, snapName string) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(volumeSnapshotGVK)
	u.SetName(snapName)
	u.SetNamespace(b.Namespace)
	_ = unstructured.SetNestedField(u.Object, b.Spec.VolumeSnapshotClassName, "spec", "volumeSnapshotClassName")
	_ = unstructured.SetNestedField(u.Object, b.Spec.PVCName, "spec", "source", "persistentVolumeClaimName")
	err := r.Create(ctx, u)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *PVCBackupReconciler) deleteSnapshot(ctx context.Context, ns, name string) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(volumeSnapshotGVK)
	u.SetName(name)
	u.SetNamespace(ns)
	return client.IgnoreNotFound(r.Delete(ctx, u))
}

func (r *PVCBackupReconciler) cloneFromSnapshot(ctx context.Context, b *operatorv1alpha1.PVCBackup, snapName, cloneName string) error {
	var src corev1.PersistentVolumeClaim
	if err := r.Get(ctx, types.NamespacedName{Name: b.Spec.PVCName, Namespace: b.Namespace}, &src); err != nil {
		return err
	}
	size := src.Spec.Resources.Requests[corev1.ResourceStorage]
	apiGroup := "snapshot.storage.k8s.io"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: cloneName, Namespace: b.Namespace},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VolumeSnapshot",
				Name:     snapName,
			},
			StorageClassName: src.Spec.StorageClassName,
		},
	}
	err := r.Create(ctx, pvc)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *PVCBackupReconciler) createResticBackupJob(ctx context.Context, b *operatorv1alpha1.PVCBackup, repo *operatorv1alpha1.BackupRepository, jobName, pvcName string) error {
	enableLinks := false
	backoff := int32(2)
	if b.Spec.BackoffLimit != nil {
		backoff = *b.Spec.BackoffLimit
	}
	ttl := int32(86400)
	if b.Spec.TTLSecondsAfterFinished != nil {
		ttl = *b.Spec.TTLSecondsAfterFinished
	}
	cmd := []string{"restic", "backup", "/data"}
	for _, ex := range b.Spec.Excludes {
		cmd = append(cmd, "--exclude", ex)
	}
	env := resticEnv(repo)
	container := corev1.Container{
		Name:    "restic",
		Image:   resticImage,
		Command: cmd,
		Env:     env,
		VolumeMounts: []corev1.VolumeMount{{
			Name: "data", MountPath: "/data",
		}},
	}
	if repo.Spec.EnvFromSecretRef != nil && repo.Spec.EnvFromSecretRef.Name != "" {
		container.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: repo.Spec.EnvFromSecretRef.Name},
		}}}
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: b.Namespace},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					EnableServiceLinks: &enableLinks,
					Containers:         []corev1.Container{container},
					Volumes: []corev1.Volume{{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName},
						},
					}},
				},
			},
		},
	}
	err := r.Create(ctx, job)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func resticEnv(repo *operatorv1alpha1.BackupRepository) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "RESTIC_REPOSITORY", Value: repo.Spec.URL},
		{Name: "RESTIC_PASSWORD", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: repo.Spec.PasswordSecretRef.Name},
				Key:                  keyOr(repo.Spec.PasswordSecretRef.Key, "RESTIC_PASSWORD"),
			},
		}},
	}
}

func (r *PVCBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.PVCBackup{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
