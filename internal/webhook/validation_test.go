package webhook_test

import (
	"encoding/json"
	"testing"

	"github.com/Reactive-Network/backrest-operator/internal/webhook"
)

func TestValidateRepository(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"url": "s3:https://example.com/bucket",
			"passwordSecretRef": map[string]string{"name": "sec"},
		},
	})
	ok, msg := webhook.ValidateObject("BackupRepository", raw)
	if !ok {
		t.Fatalf("expected ok, got %s", msg)
	}
}

func TestValidatePVCBackupNeedsVSC(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"pvcName":       "data",
			"repositoryRef": map[string]string{"name": "repo"},
			"strategy":      map[string]interface{}{"pipeline": []string{"csiSnapshot"}},
		},
	})
	ok, msg := webhook.ValidateObject("PVCBackup", raw)
	if ok {
		t.Fatal("expected failure")
	}
	if msg == "" {
		t.Fatal("expected message")
	}
}

func TestValidatePVCBackupAcceptsPVCNames(t *testing.T) {
	raw, _ := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"pvcNames":      []string{"a", "b"},
			"repositoryRef": map[string]string{"name": "repo"},
			"strategy":      map[string]interface{}{"pipeline": []string{"quiescedLive"}},
		},
	})
	ok, msg := webhook.ValidateObject("PVCBackup", raw)
	if !ok {
		t.Fatalf("expected ok, got %s", msg)
	}
}
