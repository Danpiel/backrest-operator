package controller

import (
	"context"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
)

func backupPhaseActive(phase string) bool {
	switch phase {
	case "Pending", "Flushing", "Quiescing", "Snapshotting", "Uploading", "Unquiescing":
		return true
	default:
		return false
	}
}

func restorePhaseActive(phase string) bool {
	switch phase {
	case "Pending", "Quiescing", "Restoring":
		return true
	default:
		return false
	}
}

func pvcKey(namespace, name string) string {
	return namespace + "/" + name
}

func pvcKeysOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, k := range a {
		if k == "" {
			continue
		}
		set[k] = struct{}{}
	}
	for _, k := range b {
		if _, ok := set[k]; ok {
			return true
		}
	}
	return false
}

// backupPVCKeys are the claims a PVCBackup mounts (session lock keys).
func backupPVCKeys(b *operatorv1alpha1.PVCBackup) []string {
	pvcs := pvcList(b)
	keys := make([]string, 0, len(pvcs))
	for _, p := range pvcs {
		if p == "" {
			continue
		}
		keys = append(keys, pvcKey(b.Namespace, p))
	}
	return keys
}

// restorePVCKeys are claims a PVCRestore mounts.
// fromResticToExistingPVC locks that live disk; new-PVC / VolumeSnapshot only lock the
// new claim name (no conflict with a different app PVC). SnapshotDownload never calls this.
func restorePVCKeys(r *operatorv1alpha1.PVCRestore) []string {
	switch r.Spec.Mode {
	case "fromResticToExistingPVC":
		if r.Spec.Target.ExistingPVCName == "" {
			return nil
		}
		return []string{pvcKey(r.Namespace, r.Spec.Target.ExistingPVCName)}
	case "fromResticToNewPVC", "fromVolumeSnapshot":
		name := r.Spec.Target.NewPVC.Name
		if name == "" {
			name = r.Name + "-pvc"
		}
		return []string{pvcKey(r.Namespace, name)}
	default:
		// export / unknown: no PVC session
		return nil
	}
}

type sessionHolder struct {
	Kind      string
	Namespace string
	Name      string
	Phase     string
}

func (h sessionHolder) String() string {
	return fmt.Sprintf("%s/%s/%s(%s)", h.Kind, h.Namespace, h.Name, h.Phase)
}

// listPVCSessionHolders returns active PVCBackup/PVCRestore that mount overlapping PVCs.
// SnapshotDownload is intentionally ignored (archive path on Backrest host, no app PVC mount).
func listPVCSessionHolders(ctx context.Context, c client.Client, wantKeys []string, selfKind, selfNS, selfName string) ([]sessionHolder, error) {
	if len(wantKeys) == 0 {
		return nil, nil
	}
	var out []sessionHolder

	var backups operatorv1alpha1.PVCBackupList
	if err := c.List(ctx, &backups); err != nil {
		return nil, err
	}
	for i := range backups.Items {
		b := &backups.Items[i]
		if selfKind == "PVCBackup" && b.Namespace == selfNS && b.Name == selfName {
			continue
		}
		if !backupPhaseActive(b.Status.Phase) {
			continue
		}
		if pvcKeysOverlap(wantKeys, backupPVCKeys(b)) {
			out = append(out, sessionHolder{Kind: "PVCBackup", Namespace: b.Namespace, Name: b.Name, Phase: b.Status.Phase})
		}
	}

	var restores operatorv1alpha1.PVCRestoreList
	if err := c.List(ctx, &restores); err != nil {
		return nil, err
	}
	for i := range restores.Items {
		r := &restores.Items[i]
		if selfKind == "PVCRestore" && r.Namespace == selfNS && r.Name == selfName {
			continue
		}
		if !restorePhaseActive(r.Status.Phase) {
			continue
		}
		if pvcKeysOverlap(wantKeys, restorePVCKeys(r)) {
			out = append(out, sessionHolder{Kind: "PVCRestore", Namespace: r.Namespace, Name: r.Name, Phase: r.Status.Phase})
		}
	}
	return out, nil
}

func holdersOfKind(holders []sessionHolder, kind string) []sessionHolder {
	var out []sessionHolder
	for _, h := range holders {
		if h.Kind == kind {
			out = append(out, h)
		}
	}
	return out
}

func formatHolders(holders []sessionHolder) string {
	parts := make([]string, 0, len(holders))
	for _, h := range holders {
		parts = append(parts, h.String())
	}
	return strings.Join(parts, ", ")
}

// pvcSessionBusy is true when another active backup/restore mounts an overlapping PVC.
func pvcSessionBusy(ctx context.Context, c client.Client, wantKeys []string, selfKind, selfNS, selfName string) (bool, string, error) {
	holders, err := listPVCSessionHolders(ctx, c, wantKeys, selfKind, selfNS, selfName)
	if err != nil {
		return false, "", err
	}
	if len(holders) == 0 {
		return false, "", nil
	}
	return true, formatHolders(holders), nil
}

// interruptBackup stops an in-flight backup so a restore can take the PVC mount:
// delete restic Job(s), unquiesce workloads, fail with monitoring signal.
func interruptBackup(ctx context.Context, c client.Client, scheme *runtime.Scheme, b *operatorv1alpha1.PVCBackup, reason string) error {
	br := &PVCBackupReconciler{Client: c, Scheme: scheme}
	prop := metav1.DeletePropagationBackground
	delOpts := &client.DeleteOptions{PropagationPolicy: &prop}

	if b.Status.LastJobName != "" {
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: b.Status.LastJobName, Namespace: b.Namespace}}
		_ = c.Delete(ctx, job, delOpts)
	}
	if name, ok := br.findOwnedBackupJob(ctx, b); ok {
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: b.Namespace}}
		_ = c.Delete(ctx, job, delOpts)
	}
	_, _ = br.fail(ctx, b, fmt.Errorf("%s", reason))
	return nil
}
