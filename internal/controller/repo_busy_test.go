package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
)

func TestBackupPhaseActive(t *testing.T) {
	if !backupPhaseActive("Uploading") || backupPhaseActive("Failed") || backupPhaseActive("Scheduled") {
		t.Fatal("unexpected backupPhaseActive")
	}
}

func TestRestorePhaseActive(t *testing.T) {
	if !restorePhaseActive("Restoring") || restorePhaseActive("Failed") || restorePhaseActive("Succeeded") {
		t.Fatal("unexpected restorePhaseActive")
	}
}

func TestJobRetriesDefaultZero(t *testing.T) {
	b := &operatorv1alpha1.PVCBackup{}
	if jobRetries(b) != 0 {
		t.Fatalf("default retries want 0 got %d", jobRetries(b))
	}
	var n int32 = 3
	b.Spec.Retries = &n
	if jobRetries(b) != 3 {
		t.Fatalf("retries want 3 got %d", jobRetries(b))
	}
	var legacy int32 = 2
	b.Spec.Retries = nil
	b.Spec.BackoffLimit = &legacy
	if jobRetries(b) != 2 {
		t.Fatalf("backoffLimit fallback want 2 got %d", jobRetries(b))
	}
}

func TestPVCKeysOverlap(t *testing.T) {
	if !pvcKeysOverlap([]string{"ns/a", "ns/b"}, []string{"ns/b"}) {
		t.Fatal("expected overlap")
	}
	if pvcKeysOverlap([]string{"ns/a"}, []string{"ns/c"}) {
		t.Fatal("expected no overlap")
	}
	if pvcKeysOverlap(nil, []string{"ns/a"}) {
		t.Fatal("empty should not overlap")
	}
}

func TestBackupPVCKeys(t *testing.T) {
	b := &operatorv1alpha1.PVCBackup{
		ObjectMeta: metav1.ObjectMeta{Namespace: "mainnet", Name: "geth"},
		Spec:       operatorv1alpha1.PVCBackupSpec{PVCNames: []string{"geth-data", "prysm-data"}},
	}
	keys := backupPVCKeys(b)
	if len(keys) != 2 || keys[0] != "mainnet/geth-data" || keys[1] != "mainnet/prysm-data" {
		t.Fatalf("got %v", keys)
	}
}

func TestRestorePVCKeysExistingVsNew(t *testing.T) {
	existing := &operatorv1alpha1.PVCRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "mainnet", Name: "r1"},
		Spec: operatorv1alpha1.PVCRestoreSpec{
			Mode:   "fromResticToExistingPVC",
			Target: operatorv1alpha1.RestoreTargetSpec{ExistingPVCName: "geth-data"},
		},
	}
	if got := restorePVCKeys(existing); len(got) != 1 || got[0] != "mainnet/geth-data" {
		t.Fatalf("existing: %v", got)
	}

	newPVC := &operatorv1alpha1.PVCRestore{
		ObjectMeta: metav1.ObjectMeta{Namespace: "mainnet", Name: "r2"},
		Spec: operatorv1alpha1.PVCRestoreSpec{
			Mode: "fromResticToNewPVC",
			Target: operatorv1alpha1.RestoreTargetSpec{
				NewPVC: operatorv1alpha1.NewPVCSpec{Name: "restore-scratch"},
			},
		},
	}
	got := restorePVCKeys(newPVC)
	if len(got) != 1 || got[0] != "mainnet/restore-scratch" {
		t.Fatalf("new: %v", got)
	}
	// Different disk must not overlap the live app PVC session.
	if pvcKeysOverlap(backupPVCKeys(&operatorv1alpha1.PVCBackup{
		ObjectMeta: metav1.ObjectMeta{Namespace: "mainnet"},
		Spec:       operatorv1alpha1.PVCBackupSpec{PVCName: "geth-data"},
	}), got) {
		t.Fatal("new PVC restore should not lock live geth-data session")
	}
}

func TestHoldersOfKind(t *testing.T) {
	h := []sessionHolder{
		{Kind: "PVCBackup", Name: "b1"},
		{Kind: "PVCRestore", Name: "r1"},
		{Kind: "PVCBackup", Name: "b2"},
	}
	if len(holdersOfKind(h, "PVCBackup")) != 2 {
		t.Fatal("backup holders")
	}
	if len(holdersOfKind(h, "PVCRestore")) != 1 {
		t.Fatal("restore holders")
	}
}
