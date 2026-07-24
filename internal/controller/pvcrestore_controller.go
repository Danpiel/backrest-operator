package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	case "export":
		if err := r.restoreExport(ctx, &restore); err != nil {
			return r.fail(ctx, &restore, err)
		}
	case "fromResticToNewPVC", "fromResticToExistingPVC":
		if err := r.restoreRestic(ctx, &restore); err != nil {
			return r.fail(ctx, &restore, err)
		}
	default:
		return r.fail(ctx, &restore, fmt.Errorf("unknown mode %s", mode))
	}
	restore.Status.Phase = "Succeeded"
	logger.Info("restore finished", "mode", mode, "exportURL", restore.Status.ExportURL, "job", restore.Status.LastJobName)
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
	cmd := []string{"restic", "restore", snap, "--target", "/data"}
	for _, p := range restore.Spec.Restic.PathFilters {
		cmd = append(cmd, "--include", p)
	}
	jobName := fmt.Sprintf("pvcrestore-%s-%d", restore.Name, time.Now().Unix())
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}
	enableLinks := false
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: restore.Namespace},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					EnableServiceLinks: &enableLinks,
					Containers: []corev1.Container{{
						Name:    "restic",
						Image:   resticImage,
						Command: cmd,
						Env:     resticEnv(&repo),
						VolumeMounts: []corev1.VolumeMount{{
							Name: "data", MountPath: "/data",
						}},
					}},
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

func (r *PVCRestoreReconciler) restoreExport(ctx context.Context, restore *operatorv1alpha1.PVCRestore) error {
	repoNS := restore.Spec.RepositoryRef.Namespace
	if repoNS == "" {
		repoNS = restore.Namespace
	}
	var repo operatorv1alpha1.BackupRepository
	if err := r.Get(ctx, types.NamespacedName{Name: restore.Spec.RepositoryRef.Name, Namespace: repoNS}, &repo); err != nil {
		return err
	}
	token, err := randomToken(24)
	if err != nil {
		return err
	}
	ttl := restore.Spec.Export.TTLSeconds
	if ttl == 0 {
		ttl = 3600
	}
	snap := restore.Spec.Restic.SnapshotID
	if snap == "" {
		snap = "latest"
	}
	jobName := fmt.Sprintf("export-%s-%d", restore.Name, time.Now().Unix())
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}
	labels := map[string]string{
		"app.kubernetes.io/name":      "backrest-export",
		"app.kubernetes.io/instance":  restore.Name,
		"app.kubernetes.io/component": "export",
	}
	proxyImage := exportProxyImage()
	enableLinks := false
	ttlFin := ttl
	env := append(resticEnv(&repo),
		corev1.EnvVar{Name: "EXPORT_TOKEN", Value: token},
		corev1.EnvVar{Name: "EXPORT_ONESHOT", Value: "1"},
		corev1.EnvVar{Name: "EXPORT_FILE", Value: "/work/archive.tar"},
		corev1.EnvVar{Name: "LOG_FORMAT", Value: firstNonEmpty(os.Getenv("LOG_FORMAT"), "console")},
		corev1.EnvVar{Name: "LOG_LEVEL", Value: firstNonEmpty(os.Getenv("LOG_LEVEL"), "info")},
	)
	includeArgs := ""
	for _, p := range restore.Spec.Restic.PathFilters {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		includeArgs += fmt.Sprintf(" --include %q", p)
	}
	resticScript := fmt.Sprintf(`set -euo pipefail
mkdir -p /work/out
restic restore %s --target /work/out%s
cd /work/out && tar -cf /work/archive.tar .
`, snap, includeArgs)
	if err := r.ensureRestoreRepoSecrets(ctx, restore, &repo, repoNS); err != nil {
		return err
	}
	resticInit := corev1.Container{
		Name:         "restic",
		Image:        resticImage,
		Command:      []string{"sh", "-c", resticScript},
		Env:          resticEnv(&repo),
		VolumeMounts: []corev1.VolumeMount{{Name: "work", MountPath: "/work"}},
	}
	if repo.Spec.EnvFromSecretRef != nil && repo.Spec.EnvFromSecretRef.Name != "" {
		resticInit.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: repo.Spec.EnvFromSecretRef.Name},
		}}}
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: restore.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttlFin,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					EnableServiceLinks: &enableLinks,
					InitContainers:     []corev1.Container{resticInit},
					Containers: []corev1.Container{{
						Name:    "export",
						Image:   proxyImage,
						Command: []string{"/export-proxy"},
						Env:     env,
						Ports:   []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(8080)},
							},
							PeriodSeconds: 5,
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name: "work", MountPath: "/work", ReadOnly: true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "work",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}},
				},
			},
		},
	}
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	svcName := "export-" + restore.Name
	if len(svcName) > 63 {
		svcName = svcName[:63]
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: restore.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8080, TargetPort: intstr.FromInt32(8080)}},
		},
	}
	if err := r.Create(ctx, svc); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	pathSuffix := "/" + token + "/archive.tar"
	restore.Status.LastJobName = jobName
	restore.Status.ExportURL = fmt.Sprintf("http://%s.%s.svc:8080%s", svcName, restore.Namespace, pathSuffix)
	restore.Status.ExportExpiresAt = time.Now().Add(time.Duration(ttl) * time.Second).UTC().Format(time.RFC3339)

	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("EXPORT_PUBLIC_BASE_URL")), "/"); base != "" {
		if err := r.ensureExportIngress(ctx, restore, svcName, token, labels, ttl); err != nil {
			return err
		}
		restore.Status.ExportExternalURL = base + pathSuffix
	}
	return nil
}

func (r *PVCRestoreReconciler) ensureExportIngress(ctx context.Context, restore *operatorv1alpha1.PVCRestore, svcName, token string, labels map[string]string, ttl int32) error {
	host := strings.TrimSpace(os.Getenv("EXPORT_INGRESS_HOST"))
	if host == "" {
		if u := strings.TrimSpace(os.Getenv("EXPORT_PUBLIC_BASE_URL")); u != "" {
			u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
			host = strings.Split(u, "/")[0]
		}
	}
	if host == "" {
		return fmt.Errorf("EXPORT_INGRESS_HOST or host in EXPORT_PUBLIC_BASE_URL is required for public export URLs")
	}
	className := strings.TrimSpace(os.Getenv("EXPORT_INGRESS_CLASS"))
	ingName := "export-" + restore.Name
	if len(ingName) > 63 {
		ingName = ingName[:63]
	}
	pathType := networkingv1.PathTypePrefix
	path := "/" + token
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingName,
			Namespace: restore.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"operator.backrest.io/export-ttl-seconds": fmt.Sprintf("%d", ttl),
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				Host: host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     path,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: svcName,
									Port: networkingv1.ServiceBackendPort{Number: 8080},
								},
							},
						}},
					},
				},
			}},
		},
	}
	if className != "" {
		ing.Spec.IngressClassName = &className
	}
	if raw := strings.TrimSpace(os.Getenv("EXPORT_INGRESS_ANNOTATIONS_JSON")); raw != "" {
		var ann map[string]string
		if err := json.Unmarshal([]byte(raw), &ann); err != nil {
			return fmt.Errorf("EXPORT_INGRESS_ANNOTATIONS_JSON: %w", err)
		}
		for k, v := range ann {
			ing.Annotations[k] = v
		}
	}
	if secret := strings.TrimSpace(os.Getenv("EXPORT_INGRESS_TLS_SECRET")); secret != "" {
		ing.Spec.TLS = []networkingv1.IngressTLS{{
			Hosts:      []string{host},
			SecretName: secret,
		}}
	}
	if err := r.Create(ctx, ing); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
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

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func exportProxyImage() string {
	if v := os.Getenv("EXPORT_PROXY_IMAGE"); v != "" {
		return v
	}
	if v := os.Getenv("OPERATOR_IMAGE"); v != "" {
		return v
	}
	return "ghcr.io/danpiel/backrest-operator:latest"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (r *PVCRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.PVCRestore{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
