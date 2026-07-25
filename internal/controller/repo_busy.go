package controller

import (
	"context"
	"fmt"

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

func repoRefKey(ns, name, defaultNS string) (string, string) {
	if ns == "" {
		ns = defaultNS
	}
	return ns, name
}

// repoTaskBusy reports whether another PVCBackup or PVCRestore is using the repository.
// SnapshotDownload is intentionally ignored (independent of backup/restore).
func repoTaskBusy(ctx context.Context, c client.Client, repoNS, repoName, selfKind, selfNS, selfName string) (bool, string, error) {
	var backups operatorv1alpha1.PVCBackupList
	if err := c.List(ctx, &backups); err != nil {
		return false, "", err
	}
	for i := range backups.Items {
		b := &backups.Items[i]
		if selfKind == "PVCBackup" && b.Namespace == selfNS && b.Name == selfName {
			continue
		}
		rns, rname := repoRefKey(b.Spec.RepositoryRef.Namespace, b.Spec.RepositoryRef.Name, b.Namespace)
		if rns != repoNS || rname != repoName {
			continue
		}
		if backupPhaseActive(b.Status.Phase) {
			return true, fmt.Sprintf("PVCBackup/%s/%s(%s)", b.Namespace, b.Name, b.Status.Phase), nil
		}
	}

	var restores operatorv1alpha1.PVCRestoreList
	if err := c.List(ctx, &restores); err != nil {
		return false, "", err
	}
	for i := range restores.Items {
		r := &restores.Items[i]
		if selfKind == "PVCRestore" && r.Namespace == selfNS && r.Name == selfName {
			continue
		}
		rns, rname := repoRefKey(r.Spec.RepositoryRef.Namespace, r.Spec.RepositoryRef.Name, r.Namespace)
		if rns != repoNS || rname != repoName {
			continue
		}
		if restorePhaseActive(r.Status.Phase) {
			return true, fmt.Sprintf("PVCRestore/%s/%s(%s)", r.Namespace, r.Name, r.Status.Phase), nil
		}
	}
	return false, "", nil
}
