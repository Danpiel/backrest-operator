package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
	"github.com/Danpiel/backrest-operator/internal/filters"
	"github.com/Danpiel/backrest-operator/internal/metrics"
)

const resticImage = "restic/restic:0.19.1"

type BackupRepositoryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var repo operatorv1alpha1.BackupRepository
	if err := r.Get(ctx, req.NamespacedName, &repo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !filters.ObjectAllowed(repo.Namespace, repo.Labels) {
		return ctrl.Result{}, nil
	}
	if repo.Spec.URL == "" || repo.Spec.PasswordSecretRef.Name == "" {
		repo.Status.Phase = "Failed"
		repo.Status.Conditions = []operatorv1alpha1.Condition{{Type: "Invalid", Status: "True", Message: "url and passwordSecretRef required"}}
		_ = r.Status().Update(ctx, &repo)
		return ctrl.Result{}, nil
	}
	verifyEnabled := true
	if repo.Spec.Verify.Enabled != nil {
		verifyEnabled = *repo.Spec.Verify.Enabled
	}
	repo.Status.Phase = "Ready"
	repo.Status.ResticURL = repo.Spec.URL
	if verifyEnabled {
		repo.Status.LastCheckResult = "scheduled"
		if err := r.ensureCheckCron(ctx, &repo); err != nil {
			metrics.ReconcileErrors.WithLabelValues("BackupRepository").Inc()
			logger.Error(err, "ensure check cron")
			return ctrl.Result{}, err
		}
	} else {
		repo.Status.LastCheckResult = "skipped"
	}
	return ctrl.Result{}, r.Status().Update(ctx, &repo)
}

func (r *BackupRepositoryReconciler) ensureCheckCron(ctx context.Context, repo *operatorv1alpha1.BackupRepository) error {
	schedule := repo.Spec.Verify.Schedule
	if schedule == "" {
		schedule = "0 3 * * 0"
	}
	name := "restic-check-" + repo.Name
	env := []corev1.EnvVar{
		{Name: "RESTIC_REPOSITORY", Value: repo.Spec.URL},
		{Name: "RESTIC_PASSWORD", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: repo.Spec.PasswordSecretRef.Name},
				Key:                  keyOr(repo.Spec.PasswordSecretRef.Key, "RESTIC_PASSWORD"),
			},
		}},
	}
	container := corev1.Container{
		Name:    "restic",
		Image:   resticImage,
		Command: []string{"restic", "check"},
		Env:     env,
	}
	if repo.Spec.EnvFromSecretRef != nil && repo.Spec.EnvFromSecretRef.Name != "" {
		container.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: repo.Spec.EnvFromSecretRef.Name}}}}
	}
	enableLinks := false
	cron := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: repo.Namespace},
		Spec: batchv1.CronJobSpec{
			Schedule:          schedule,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							EnableServiceLinks: &enableLinks,
							Containers:         []corev1.Container{container},
						},
					},
				},
			},
		},
	}
	_ = controllerutil.SetControllerReference(repo, cron, r.Scheme)
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

func keyOr(k, def string) string {
	if k == "" {
		return def
	}
	return k
}

func (r *BackupRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.BackupRepository{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}

// --- BackupPlan ---

type BackupPlanReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *BackupPlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var plan operatorv1alpha1.BackupPlan
	if err := r.Get(ctx, req.NamespacedName, &plan); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !filters.ObjectAllowed(plan.Namespace, plan.Labels) {
		return ctrl.Result{}, nil
	}
	if plan.Spec.RepositoryRef.Name == "" {
		plan.Status.Phase = "Failed"
		return ctrl.Result{}, r.Status().Update(ctx, &plan)
	}
	targetNS := plan.Namespace
	if plan.Spec.ClusterRef.Namespace != "" {
		targetNS = plan.Spec.ClusterRef.Namespace
	}
	key := fmt.Sprintf("%s.%s.json", plan.Namespace, plan.Name)
	fragmentObj := map[string]interface{}{
		"id":        plan.Namespace + "-" + plan.Name,
		"repo":      plan.Spec.RepositoryRef.Name,
		"paths":     orStrings(plan.Spec.Paths),
		"excludes":  orStrings(plan.Spec.Excludes),
		"schedule":  plan.Spec.Schedule,
		"retention": orMap(plan.Spec.Retention),
		"hooks":     orHooks(plan.Spec.Hooks),
		"tags":      orStrings(plan.Spec.Tags),
	}
	fragmentBytes, err := jsonMarshal(fragmentObj)
	if err != nil {
		return ctrl.Result{}, err
	}
	fragment := string(fragmentBytes)

	cmName := "backrest-plans"
	var cm corev1.ConfigMap
	err = r.Get(ctx, client.ObjectKey{Namespace: targetNS, Name: cmName}, &cm)
	if apierrors.IsNotFound(err) {
		cm = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: targetNS},
			Data:       map[string]string{key: fragment},
		}
		if err := r.Create(ctx, &cm); err != nil {
			metrics.ReconcileErrors.WithLabelValues("BackupPlan").Inc()
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	} else {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[key] = fragment
		if err := r.Update(ctx, &cm); err != nil {
			return ctrl.Result{}, err
		}
	}
	plan.Status.Phase = "Ready"
	return ctrl.Result{}, r.Status().Update(ctx, &plan)
}

func orStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func orMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

func orHooks(v []map[string]interface{}) []map[string]interface{} {
	if v == nil {
		return []map[string]interface{}{}
	}
	return v
}

func (r *BackupPlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.BackupPlan{}).
		Complete(r)
}
