package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/Reactive-Network/backrest-operator/api/v1alpha1"
	"github.com/Reactive-Network/backrest-operator/internal/backrest"
)

func hostClientForCluster(clusterNS, clusterName string) *backrest.Client {
	return backrest.NewClient(backrest.HostURL(clusterNS, clusterName))
}

func resolveClusterRef(ref operatorv1alpha1.ObjectReference, defaultNS string) (ns, name string) {
	ns = ref.Namespace
	if ns == "" {
		ns = defaultNS
	}
	name = ref.Name
	if name == "" {
		name = "main"
	}
	return ns, name
}

func instanceForCluster(name string) string {
	// Backrest requires a non-empty instance; use the BackrestCluster name.
	return name
}

func planIDForPVCBackup(b *operatorv1alpha1.PVCBackup) string {
	return sanitizeID(b.Namespace + "-" + b.Name)
}

func sanitizeID(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func pvcBackupPaths(b *operatorv1alpha1.PVCBackup) []string {
	pvcs := pvcList(b)
	paths := b.Spec.Paths
	if len(paths) == 1 && (paths[0] == "/" || paths[0] == "") {
		paths = nil
	}
	if len(paths) == 0 {
		out := make([]string, 0, len(pvcs))
		for _, pvc := range pvcs {
			out = append(out, "/data/"+sanitizePath(pvc))
		}
		return out
	}
	return paths
}

func readSecretKey(ctx context.Context, c client.Client, ns, name, key string) (string, error) {
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &sec); err != nil {
		return "", err
	}
	if key == "" {
		key = "RESTIC_PASSWORD"
	}
	v, ok := sec.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s missing key %s", ns, name, key)
	}
	return string(v), nil
}

func secretEnvLines(ctx context.Context, c client.Client, ns, name string) ([]string, error) {
	var sec corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &sec); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(sec.Data))
	for k, v := range sec.Data {
		out = append(out, k+"="+string(v))
	}
	return out, nil
}

func syncRepositoryToHost(ctx context.Context, c client.Client, repo *operatorv1alpha1.BackupRepository) error {
	if !repo.Spec.Backrest.SyncToHost {
		return nil
	}
	logger := log.FromContext(ctx).WithValues("backuprepository", client.ObjectKeyFromObject(repo))
	clusterNS, clusterName := resolveClusterRef(repo.Spec.Backrest.ClusterRef, repo.Namespace)
	bc := hostClientForCluster(clusterNS, clusterName)
	instance := instanceForCluster(clusterName)
	if err := bc.EnsureInstance(ctx, instance); err != nil {
		return fmt.Errorf("ensure backrest instance: %w", err)
	}
	pw, err := readSecretKey(ctx, c, repo.Namespace, repo.Spec.PasswordSecretRef.Name, keyOr(repo.Spec.PasswordSecretRef.Key, "RESTIC_PASSWORD"))
	if err != nil {
		return err
	}
	brRepo := backrest.Repo{
		ID:             repo.Name,
		URI:            repo.Spec.URL,
		Password:       pw,
		AutoInitialize: false,
		Shared:         repo.Spec.Shared,
	}
	if repo.Spec.EnvFromSecretRef != nil && repo.Spec.EnvFromSecretRef.Name != "" {
		env, err := secretEnvLines(ctx, c, repo.Namespace, repo.Spec.EnvFromSecretRef.Name)
		if err != nil {
			return err
		}
		brRepo.Env = env
	}
	if err := bc.UpsertRepo(ctx, brRepo); err != nil {
		return fmt.Errorf("upsert repo on host: %w", err)
	}
	if err := bc.IndexSnapshots(ctx, repo.Name); err != nil {
		logger.Error(err, "index snapshots after repo sync (non-fatal)")
	}
	logger.V(1).Info("synced repository to Backrest UI", "cluster", clusterNS+"/"+clusterName, "repo", repo.Name)
	return nil
}

func syncPlanToHost(ctx context.Context, c client.Client, plan *operatorv1alpha1.BackupPlan, repo *operatorv1alpha1.BackupRepository) error {
	clusterNS, clusterName := resolveClusterRef(plan.Spec.ClusterRef, plan.Namespace)
	if repo != nil && repo.Spec.Backrest.ClusterRef.Name != "" {
		clusterNS, clusterName = resolveClusterRef(repo.Spec.Backrest.ClusterRef, repo.Namespace)
	}
	bc := hostClientForCluster(clusterNS, clusterName)
	if err := bc.EnsureInstance(ctx, instanceForCluster(clusterName)); err != nil {
		return err
	}
	repoID := plan.Spec.RepositoryRef.Name
	retention := map[string]interface{}{}
	if plan.Spec.Retention != nil {
		if v, ok := plan.Spec.Retention["keepLast"]; ok {
			retention["policyKeepLastN"] = v
		} else {
			retention = plan.Spec.Retention
		}
	}
	schedule := map[string]interface{}{"disabled": true}
	if plan.Spec.Schedule != "" {
		// Host cannot see PVC mounts; keep schedule disabled and let PVCBackup own timing.
		schedule = map[string]interface{}{"disabled": true}
	}
	bp := backrest.Plan{
		ID:        sanitizeID(plan.Namespace + "-" + plan.Name),
		Repo:      repoID,
		Paths:     orStrings(plan.Spec.Paths),
		Excludes:  orStrings(plan.Spec.Excludes),
		Schedule:  schedule,
		Retention: retention,
	}
	if err := bc.UpsertPlan(ctx, bp); err != nil {
		return err
	}
	log.FromContext(ctx).V(1).Info("synced plan to Backrest UI", "plan", bp.ID, "repo", repoID)
	return nil
}

func syncPVCBackupPlanToHost(ctx context.Context, c client.Client, b *operatorv1alpha1.PVCBackup, repo *operatorv1alpha1.BackupRepository) error {
	if repo == nil || !repo.Spec.Backrest.SyncToHost {
		return nil
	}
	clusterNS, clusterName := resolveClusterRef(repo.Spec.Backrest.ClusterRef, repo.Namespace)
	bc := hostClientForCluster(clusterNS, clusterName)
	if err := bc.EnsureInstance(ctx, instanceForCluster(clusterName)); err != nil {
		return err
	}
	retention := map[string]interface{}{}
	if b.Spec.Retention.KeepLast != nil {
		retention["policyKeepLastN"] = *b.Spec.Retention.KeepLast
	}
	plan := backrest.Plan{
		ID:        planIDForPVCBackup(b),
		Repo:      repo.Name,
		Paths:     pvcBackupPaths(b),
		Excludes:  orStrings(b.Spec.Excludes),
		Schedule:  map[string]interface{}{"disabled": true},
		Retention: retention,
	}
	if err := bc.UpsertPlan(ctx, plan); err != nil {
		return err
	}
	if err := bc.IndexSnapshots(ctx, repo.Name); err != nil {
		log.FromContext(ctx).Error(err, "index snapshots after plan sync")
	}
	log.FromContext(ctx).V(1).Info("synced PVCBackup plan to Backrest UI", "plan", plan.ID, "repo", repo.Name)
	return nil
}

func removePVCBackupPlanFromHost(ctx context.Context, c client.Client, b *operatorv1alpha1.PVCBackup) error {
	var repo operatorv1alpha1.BackupRepository
	ns := b.Spec.RepositoryRef.Namespace
	if ns == "" {
		ns = b.Namespace
	}
	if err := c.Get(ctx, types.NamespacedName{Name: b.Spec.RepositoryRef.Name, Namespace: ns}, &repo); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !repo.Spec.Backrest.SyncToHost {
		return nil
	}
	clusterNS, clusterName := resolveClusterRef(repo.Spec.Backrest.ClusterRef, repo.Namespace)
	bc := hostClientForCluster(clusterNS, clusterName)
	planID := planIDForPVCBackup(b)
	if err := bc.DeletePlan(ctx, planID); err != nil {
		return err
	}
	log.FromContext(ctx).Info("removed PVCBackup plan from Backrest host", "plan", planID, "repo", repo.Name)
	return nil
}
