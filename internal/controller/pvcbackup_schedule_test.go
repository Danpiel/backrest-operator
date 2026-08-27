package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/Reactive-Network/backrest-operator/api/v1alpha1"
)

func TestScheduleDueSkipsImmediateRetryAfterFailure(t *testing.T) {
	b := &operatorv1alpha1.PVCBackup{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(time.Now().Add(-48 * time.Hour)),
		},
		Spec: operatorv1alpha1.PVCBackupSpec{
			Schedule: "0 */12 * * *",
		},
		Status: operatorv1alpha1.PVCBackupStatus{
			Phase:          "Failed",
			LastBackupTime: time.Now().UTC().Format(time.RFC3339),
		},
	}
	due, wait, err := scheduleDue(b)
	if err != nil {
		t.Fatalf("scheduleDue: %v", err)
	}
	if due {
		t.Fatalf("expected not due immediately after failed attempt, wait=%s", wait)
	}
	if wait < time.Minute {
		t.Fatalf("expected wait until next cron slot, got %s", wait)
	}
}

func TestForceRunPendingOverridesSchedule(t *testing.T) {
	b := &operatorv1alpha1.PVCBackup{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{annForceRun: "token-2"},
		},
		Spec: operatorv1alpha1.PVCBackupSpec{
			Schedule: "0 */12 * * *",
		},
		Status: operatorv1alpha1.PVCBackupStatus{
			Phase:          "Failed",
			LastBackupTime: time.Now().UTC().Format(time.RFC3339),
			LastForceRun:   "token-1",
		},
	}
	due, _, err := scheduleDue(b)
	if err != nil {
		t.Fatalf("scheduleDue: %v", err)
	}
	if !due {
		t.Fatal("expected force-run to make backup due")
	}
	if !forceRunPending(b) {
		t.Fatal("expected forceRunPending")
	}
}
