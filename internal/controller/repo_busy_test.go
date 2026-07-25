package controller

import (
	"testing"

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
