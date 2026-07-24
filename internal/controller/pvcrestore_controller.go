package controller

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
	"github.com/Danpiel/backrest-operator/internal/filters"
	"github.com/Danpiel/backrest-operator/internal/metrics"
)

type PVCRestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *PVCRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("pvcrestore", req.NamespacedName)
	var restore operatorv1alpha1.PVCRestore
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !filters.ObjectAllowed(restore.Namespace, restore.Labels) {
		logger.V(1).Info("skipped by watch filter")
		return ctrl.Result{}, nil
	}
	if restore.Status.Phase == "Succeeded" {
		return ctrl.Result{}, nil
	}

	mode := restore.Spec.Mode
	logger.Info("restore started", "mode", mode)
	switch mode {
	case "fromVolumeSnapshot":
		if err := r.restoreFromSnapshot(ctx, &restore); err != nil {
			return r.fail(ctx, &restore, err)
		}
	case "fromResticToNewPVC", "fromResticToExistingPVC":
		if err := r.restoreRestic(ctx, &restore); err != nil {
			return r.fail(ctx, &restore, err)
		}
	case "export":
		return r.fail(ctx, &restore, fmt.Errorf("mode export is removed; use Backrest GetDownloadURL / MCP get_snapshot_download_url"))
	default:
		return r.fail(ctx, &restore, fmt.Errorf("unknown mode %s", mode))
	}
	restore.Status.Phase = "Succeeded"
	logger.Info("restore finished", "mode", mode, "job", restore.Status.LastJobName)
	return ctrl.Result{}, r.Status().Update(ctx, &restore)
}

func (r *PVCRestoreReconciler) fail(ctx context.Context, restore *operatorv1alpha1.PVCRestore, err error) (ctrl.Result, error) {
	log.FromContext(ctx).Error(err, "restore failed", "pvcrestore", client.ObjectKeyFromObject(restore))
	metrics.ReconcileErrors.WithLabelValues("PVCRestore").Inc()
	restore.Status.Phase = "Failed"
	restore.Status.Conditions = []operatorv1alpha1.Condition{{Type: "Failed", Status: "True", Message: err.Error()}}
	_ = r.Status().Update(ctx, restore)
	return ctrl.Result{}, err
}

func (r *PVCRestoreReconciler) restoreFromSnapshot(ctx context.Context, restore *operatorv1alpha1.PVCRestore) error {
	name := restore.Spec.Target.NewPVC.Name
	if name == "" {
		name = restore.Name + "-pvc"
	}
	size := restore.Spec.Target.NewPVC.Size
	if size == "" {
		size = "10Gi"
	}
	apiGroup := "snapshot.storage.k8s.io"
	modes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: restore.Namespace},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: modes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
			},
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VolumeSnapshot",
				Name:     restore.Spec.VolumeSnapshotRef.Name,
			},
		},
	}
	if sc := restore.Spec.Target.NewPVC.StorageClassName; sc != "" {
		pvc.Spec.StorageClassName = &sc
	}
	err := r.Create(ctx, pvc)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (r *PVCRestoreReconciler) restoreRestic(ctx context.Context, restore *operatorv1alpha1.PVCRestore) error {
	repoNS := restore.Spec.RepositoryRef.Namespace
	if repoNS == "" {
		repoNS = restore.Namespace
	}
	var repo operatorv1alpha1.BackupRepository
	if err := r.Get(ctx, types.NamespacedName{Name: restore.Spec.RepositoryRef.Name, Namespace: repoNS}, &repo); err != nil {
		return err
	}
	var pvcName string
	var quiesceState map[string]int32
	if restore.Spec.Mode == "fromResticToNewPVC" {
		pvcName = restore.Spec.Target.NewPVC.Name
		if pvcName == "" {
			pvcName = restore.Name + "-pvc"
		}
		size := restore.Spec.Target.NewPVC.Size
		if size == "" {
			size = "10Gi"
		}
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: restore.Namespace},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
				},
			},
		}
		if sc := restore.Spec.Target.NewPVC.StorageClassName; sc != "" {
			pvc.Spec.StorageClassName = &sc
		}
		if err := r.Create(ctx, pvc); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	} else {
		pvcName = restore.Spec.Target.ExistingPVCName
		if pvcName == "" {
			return fmt.Errorf("target.existingPVCName required")
		}
		if restore.Spec.Quiesce.Enabled {
			// reuse PVCBackup reconciler helpers via local scale
			br := &PVCBackupReconciler{Client: r.Client, Scheme: r.Scheme}
			fake := &operatorv1alpha1.PVCBackup{
				ObjectMeta: metav1.ObjectMeta{Namespace: restore.Namespace},
				Spec:       operatorv1alpha1.PVCBackupSpec{Quiesce: restore.Spec.Quiesce},
			}
			var err error
			quiesceState, err = br.quiesce(ctx, fake)
			if err != nil {
				return err
			}
			defer func() { _ = br.unquiesce(ctx, restore.Namespace, quiesceState) }()
		}
	}
	snap := restore.Spec.Restic.SnapshotID
	if snap == "" {
		snap = "latest"
	}
	if err := r.ensureRestoreRepoSecrets(ctx, restore, &repo, repoNS); err != nil {
		return err
	}
	cmd := []string{"sh", "-ec", "restic restore " + snap + " --target /data"}
	for _, p := range restore.Spec.Restic.PathFilters {
		cmd[2] += " --include " + shellQuoteOne(p)
	}
	jobName := fmt.Sprintf("pvcrestore-%s-%d", restore.Name, time.Now().Unix())
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}
	enableLinks := false
	resticContainer := corev1.Container{
		Name:    "restic",
		Image:   resticImage,
		Command: cmd,
		Env:     resticEnv(&repo),
		VolumeMounts: []corev1.VolumeMount{{
			Name: "data", MountPath: "/data",
		}},
	}
	if repo.Spec.EnvFromSecretRef != nil && repo.Spec.EnvFromSecretRef.Name != "" {
		resticContainer.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: repo.Spec.EnvFromSecretRef.Name},
		}}}
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: restore.Namespace},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					EnableServiceLinks: &enableLinks,
					Containers:         []corev1.Container{resticContainer},
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
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	restore.Status.LastJobName = jobName
	return nil
}

func (r *PVCRestoreReconciler) ensureRestoreRepoSecrets(ctx context.Context, restore *operatorv1alpha1.PVCRestore, repo *operatorv1alpha1.BackupRepository, repoNS string) error {
	if repoNS == restore.Namespace {
		return nil
	}
	if err := r.mirrorSecret(ctx, repoNS, repo.Spec.PasswordSecretRef.Name, restore.Namespace, repo.Spec.PasswordSecretRef.Name); err != nil {
		return err
	}
	if repo.Spec.EnvFromSecretRef != nil && repo.Spec.EnvFromSecretRef.Name != "" {
		if err := r.mirrorSecret(ctx, repoNS, repo.Spec.EnvFromSecretRef.Name, restore.Namespace, repo.Spec.EnvFromSecretRef.Name); err != nil {
			return err
		}
	}
	return nil
}

func (r *PVCRestoreReconciler) mirrorSecret(ctx context.Context, srcNS, srcName, dstNS, dstName string) error {
	var src corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: srcNS, Name: srcName}, &src); err != nil {
		return err
	}
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: dstName, Namespace: dstNS},
		Type:       src.Type,
		Data:       src.Data,
	}
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

func (r *PVCRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.PVCRestore{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
