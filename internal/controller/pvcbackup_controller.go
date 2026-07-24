package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
	"github.com/Danpiel/backrest-operator/internal/filters"
	"github.com/Danpiel/backrest-operator/internal/metrics"
)

const (
	annForceRun  = "operator.backrest.io/force-run"
	kubectlImage = "bitnami/kubectl:1.32"
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

	pvcs := pvcList(&backup)
	if len(pvcs) == 0 {
		return r.fail(ctx, &backup, fmt.Errorf("pvcName or pvcNames required"))
	}

	if backup.Spec.Schedule != "" {
		if err := r.ensureScheduleCron(ctx, &backup); err != nil {
			return r.fail(ctx, &backup, err)
		}
		force := ""
		if backup.Annotations != nil {
			force = backup.Annotations[annForceRun]
		}
		if force == "" || force == backup.Status.LastForceRun {
			if backup.Status.Phase == "" || backup.Status.Phase == "Failed" {
				backup.Status.Phase = "Scheduled"
				_ = r.Status().Update(ctx, &backup)
			}
			return ctrl.Result{}, nil
		}
	} else if backup.Status.Phase == "Succeeded" {
		return ctrl.Result{}, nil
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
	if err := r.ensureRepoSecrets(ctx, &backup, &repo); err != nil {
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

	uploadPVCs := pvcs
	var snapName string
	for _, step := range pipeline {
		if step == "csiSnapshot" || step == "topolvmSnapshot" {
			if len(pvcs) != 1 {
				return r.fail(ctx, &backup, fmt.Errorf("csiSnapshot supports a single pvcName; use quiescedLive for multi-PVC"))
			}
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
			uploadPVCs = []string{clone}
		}
	}

	backup.Status.Phase = "Uploading"
	_ = r.Status().Update(ctx, &backup)
	jobName := fmt.Sprintf("pvcbackup-%s-%d", backup.Name, started.Unix())
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}
	if err := r.createResticBackupJob(ctx, &backup, &repo, jobName, uploadPVCs); err != nil {
		return r.fail(ctx, &backup, err)
	}
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: backup.Namespace}, &job); err == nil {
		if job.Status.Succeeded > 0 {
			dur := time.Since(started).Seconds()
			metrics.BackupTotal.WithLabelValues(backup.Namespace, backup.Name, "success").Inc()
			metrics.BackupDuration.WithLabelValues(backup.Namespace, backup.Name).Observe(dur)
			metrics.BackupLastSuccess.WithLabelValues(backup.Namespace, backup.Name).Set(float64(time.Now().Unix()))
			if force := backup.Annotations[annForceRun]; force != "" {
				backup.Status.LastForceRun = force
			}
			backup.Status.Phase = "Succeeded"
			if backup.Spec.Schedule != "" {
				backup.Status.Phase = "Scheduled"
			}
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

func pvcList(b *operatorv1alpha1.PVCBackup) []string {
	if len(b.Spec.PVCNames) > 0 {
		return append([]string{}, b.Spec.PVCNames...)
	}
	if b.Spec.PVCName != "" {
		return []string{b.Spec.PVCName}
	}
	return nil
}

func (r *PVCBackupReconciler) ensureScheduleCron(ctx context.Context, b *operatorv1alpha1.PVCBackup) error {
	saName := "pvcbackup-" + b.Name
	if len(saName) > 63 {
		saName = saName[:63]
	}
	cronName := saName
	labels := map[string]string{
		"app.kubernetes.io/name":       "backrest",
		"app.kubernetes.io/component":  "pvcbackup-schedule",
		"app.kubernetes.io/managed-by": "backrest-operator",
		"operator.backrest.io/pvcbackup": b.Name,
	}

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: b.Namespace, Labels: labels}}
	_ = controllerutil.SetControllerReference(b, sa, r.Scheme)
	if err := r.createOrIgnore(ctx, sa); err != nil {
		return err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: b.Namespace, Labels: labels},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{"operator.backrest.io"},
			Resources:     []string{"pvcbackups"},
			ResourceNames: []string{b.Name},
			Verbs:         []string{"get", "patch", "update"},
		}},
	}
	_ = controllerutil.SetControllerReference(b, role, r.Scheme)
	if err := r.createOrUpdateRole(ctx, role); err != nil {
		return err
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: b.Namespace, Labels: labels},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: saName},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: saName, Namespace: b.Namespace}},
	}
	_ = controllerutil.SetControllerReference(b, rb, r.Scheme)
	if err := r.createOrUpdateRoleBinding(ctx, rb); err != nil {
		return err
	}

	enableLinks := false
	script := fmt.Sprintf(
		`kubectl -n %s annotate pvcbackup %s %s="$(date -u +%%Y%%m%%d%%H%%M%%S)" --overwrite`,
		b.Namespace, b.Name, annForceRun,
	)
	cron := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: cronName, Namespace: b.Namespace, Labels: labels},
		Spec: batchv1.CronJobSpec{
			Schedule:          b.Spec.Schedule,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							ServiceAccountName: saName,
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							EnableServiceLinks: &enableLinks,
							Containers: []corev1.Container{{
								Name:    "trigger",
								Image:   kubectlImage,
								Command: []string{"/bin/bash", "-ec", script},
							}},
						},
					},
				},
			},
		},
	}
	_ = controllerutil.SetControllerReference(b, cron, r.Scheme)
	var cur batchv1.CronJob
	err := r.Get(ctx, client.ObjectKeyFromObject(cron), &cur)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, cron)
	}
	if err != nil {
		return err
	}
	cur.Spec = cron.Spec
	return r.Update(ctx, &cur)
}

func (r *PVCBackupReconciler) createOrIgnore(ctx context.Context, obj client.Object) error {
	err := r.Create(ctx, obj)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *PVCBackupReconciler) createOrUpdateRole(ctx context.Context, desired *rbacv1.Role) error {
	var cur rbacv1.Role
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &cur)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	cur.Rules = desired.Rules
	return r.Update(ctx, &cur)
}

func (r *PVCBackupReconciler) createOrUpdateRoleBinding(ctx context.Context, desired *rbacv1.RoleBinding) error {
	var cur rbacv1.RoleBinding
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &cur)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	cur.RoleRef = desired.RoleRef
	cur.Subjects = desired.Subjects
	return r.Update(ctx, &cur)
}

func (r *PVCBackupReconciler) ensureRepoSecrets(ctx context.Context, b *operatorv1alpha1.PVCBackup, repo *operatorv1alpha1.BackupRepository) error {
	repoNS := repo.Namespace
	if repoNS == b.Namespace {
		return nil
	}
	// Mirror password + env secrets into the PVC namespace so Jobs can mount them.
	if err := r.mirrorSecret(ctx, repoNS, repo.Spec.PasswordSecretRef.Name, b.Namespace, repo.Spec.PasswordSecretRef.Name, b); err != nil {
		return err
	}
	if repo.Spec.EnvFromSecretRef != nil && repo.Spec.EnvFromSecretRef.Name != "" {
		if err := r.mirrorSecret(ctx, repoNS, repo.Spec.EnvFromSecretRef.Name, b.Namespace, repo.Spec.EnvFromSecretRef.Name, b); err != nil {
			return err
		}
	}
	return nil
}

func (r *PVCBackupReconciler) mirrorSecret(ctx context.Context, srcNS, srcName, dstNS, dstName string, owner client.Object) error {
	var src corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: srcNS, Name: srcName}, &src); err != nil {
		return err
	}
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: dstName, Namespace: dstNS},
		Type:       src.Type,
		Data:       src.Data,
	}
	_ = controllerutil.SetControllerReference(owner, desired, r.Scheme)
	var cur corev1.Secret
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &cur)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	cur.Data = src.Data
	cur.Type = src.Type
	return r.Update(ctx, &cur)
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
		parts := split3(key)
		if len(parts) != 3 {
			continue
		}
		_, _ = r.scaleWorkload(ctx, parts[0], parts[1], parts[2], replicas)
	}
	return nil
}

func split3(s string) []string {
	return strings.SplitN(s, "/", 3)
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
	u.Object["spec"] = map[string]interface{}{
		"volumeSnapshotClassName": b.Spec.VolumeSnapshotClassName,
		"source": map[string]interface{}{
			"persistentVolumeClaimName": b.Spec.PVCName,
		},
	}
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
	clone := src.DeepCopy()
	clone.ObjectMeta = metav1.ObjectMeta{Name: cloneName, Namespace: b.Namespace}
	clone.Spec.VolumeName = ""
	clone.Spec.DataSource = &corev1.TypedLocalObjectReference{
		APIGroup: strPtr("snapshot.storage.k8s.io"),
		Kind:     "VolumeSnapshot",
		Name:     snapName,
	}
	clone.Status = corev1.PersistentVolumeClaimStatus{}
	err := r.Create(ctx, clone)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func strPtr(s string) *string { return &s }

func (r *PVCBackupReconciler) createResticBackupJob(ctx context.Context, b *operatorv1alpha1.PVCBackup, repo *operatorv1alpha1.BackupRepository, jobName string, pvcNames []string) error {
	enableLinks := false
	backoff := int32(2)
	if b.Spec.BackoffLimit != nil {
		backoff = *b.Spec.BackoffLimit
	}
	ttl := int32(86400)
	if b.Spec.TTLSecondsAfterFinished != nil {
		ttl = *b.Spec.TTLSecondsAfterFinished
	}

	vols := []corev1.Volume{}
	mounts := []corev1.VolumeMount{}
	paths := b.Spec.Paths
	if len(paths) == 0 {
		paths = nil
		for i, pvc := range pvcNames {
			volName := fmt.Sprintf("data-%d", i)
			mountPath := "/data/" + sanitizePath(pvc)
			vols = append(vols, corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc, ReadOnly: true},
				},
			})
			mounts = append(mounts, corev1.VolumeMount{Name: volName, MountPath: mountPath, ReadOnly: true})
			paths = append(paths, mountPath)
		}
	} else {
		for i, pvc := range pvcNames {
			volName := fmt.Sprintf("data-%d", i)
			vols = append(vols, corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc, ReadOnly: true},
				},
			})
			mounts = append(mounts, corev1.VolumeMount{Name: volName, MountPath: paths[min(i, len(paths)-1)], ReadOnly: true})
		}
	}

	cmd := []string{"sh", "-ec", "restic snapshots >/dev/null 2>&1 || restic init; restic backup " + strings.Join(shellQuote(paths), " ")}
	for _, ex := range b.Spec.Excludes {
		cmd[2] += " --exclude " + shellQuoteOne(ex)
	}
	if b.Spec.Retention.KeepLast != nil {
		cmd[2] += fmt.Sprintf(" && restic forget --keep-last %d --prune", *b.Spec.Retention.KeepLast)
	}

	env := resticEnv(repo)
	container := corev1.Container{
		Name:         "restic",
		Image:        resticImage,
		Command:      cmd,
		Env:          env,
		VolumeMounts: mounts,
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
					NodeSelector:       b.Spec.NodeSelector,
					Containers:         []corev1.Container{container},
					Volumes:            vols,
				},
			},
		},
	}
	_ = controllerutil.SetControllerReference(b, job, r.Scheme)
	err := r.Create(ctx, job)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func sanitizePath(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	if s == "" {
		return "vol"
	}
	return s
}

func shellQuote(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = shellQuoteOne(p)
	}
	return out
}

func shellQuoteOne(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
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
		Owns(&batchv1.CronJob{}).
		Complete(r)
}
