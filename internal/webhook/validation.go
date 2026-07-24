package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
)

const leaveDownAnnotation = "operator.backrest.io/leave-down-confirmed"

var allowedURLPrefixes = []string{"s3:", "b2:", "azure:", "gs:", "sftp:", "rclone:", "rest:", "/", "local:"}

// ValidateObject validates a CR object by kind.
func ValidateObject(kind string, raw []byte) (bool, string) {
	switch kind {
	case "BackupRepository":
		var obj operatorv1alpha1.BackupRepository
		if err := json.Unmarshal(raw, &obj); err != nil {
			return false, err.Error()
		}
		if obj.Spec.URL == "" {
			return false, "spec.url is required"
		}
		ok := false
		for _, p := range allowedURLPrefixes {
			if strings.HasPrefix(obj.Spec.URL, p) {
				ok = true
				break
			}
		}
		if !ok {
			return false, "unsupported repository URL scheme"
		}
		if obj.Spec.PasswordSecretRef.Name == "" {
			return false, "spec.passwordSecretRef.name is required"
		}
	case "PVCBackup":
		var obj operatorv1alpha1.PVCBackup
		if err := json.Unmarshal(raw, &obj); err != nil {
			return false, err.Error()
		}
		if obj.Spec.PVCName == "" {
			return false, "spec.pvcName is required"
		}
		if obj.Spec.RepositoryRef.Name == "" {
			return false, "spec.repositoryRef.name is required"
		}
		for _, s := range obj.Spec.Strategy.Pipeline {
			if s == "csiSnapshot" || s == "topolvmSnapshot" {
				if obj.Spec.VolumeSnapshotClassName == "" {
					return false, "volumeSnapshotClassName required for snapshot strategies"
				}
			}
		}
		if obj.Spec.Quiesce.LeaveDown && obj.Annotations[leaveDownAnnotation] != "true" {
			return false, fmt.Sprintf("leaveDown requires annotation %s=true", leaveDownAnnotation)
		}
	case "PVCRestore":
		var obj operatorv1alpha1.PVCRestore
		if err := json.Unmarshal(raw, &obj); err != nil {
			return false, err.Error()
		}
		switch obj.Spec.Mode {
		case "fromResticToNewPVC", "fromResticToExistingPVC", "export":
			if obj.Spec.RepositoryRef.Name == "" {
				return false, "spec.repositoryRef.name is required"
			}
		case "fromVolumeSnapshot":
			if obj.Spec.VolumeSnapshotRef.Name == "" {
				return false, "volumeSnapshotRef.name required"
			}
		}
		ttl := obj.Spec.Export.TTLSeconds
		if obj.Spec.Mode == "export" || obj.Spec.Export.Enabled {
			if ttl == 0 {
				ttl = 3600
			}
			if ttl < 60 || ttl > 86400 {
				return false, "export.ttlSeconds must be between 60 and 86400"
			}
		}
	case "BackupPlan":
		var obj operatorv1alpha1.BackupPlan
		if err := json.Unmarshal(raw, &obj); err != nil {
			return false, err.Error()
		}
		if obj.Spec.RepositoryRef.Name == "" {
			return false, "spec.repositoryRef.name is required"
		}
	}
	return true, ""
}

// HandleAdmission processes an AdmissionReview body.
func HandleAdmission(body []byte) []byte {
	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		return mustReview("", false, err.Error())
	}
	req := review.Request
	if req == nil {
		return mustReview("", false, "missing request")
	}
	raw := req.Object.Raw
	if len(raw) == 0 {
		raw = req.OldObject.Raw
	}
	ok, msg := ValidateObject(req.Kind.Kind, raw)
	resp := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Response: &admissionv1.AdmissionResponse{UID: req.UID, Allowed: ok},
	}
	if msg != "" {
		resp.Response.Result = &metav1.Status{Message: msg}
	}
	out, _ := json.Marshal(resp)
	return out
}

func mustReview(uid string, allowed bool, msg string) []byte {
	resp := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Response: &admissionv1.AdmissionResponse{Allowed: allowed, Result: &metav1.Status{Message: msg}},
	}
	out, _ := json.Marshal(resp)
	return out
}

// Handler returns HTTP routes for validating admission.
func Handler() http.Handler {
	mux := http.NewServeMux()
	h := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(HandleAdmission(body))
	}
	mux.HandleFunc("/validate", h)
	mux.HandleFunc("/", h)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	return mux
}
