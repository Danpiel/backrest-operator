package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// SnapshotDownloadSpec requests a signed Backrest download URL for a restic snapshot.
type SnapshotDownloadSpec struct {
	// RepositoryRef points at the BackupRepository (restic repo id = CR name when synced).
	RepositoryRef ObjectReference `json:"repositoryRef"`
	// SnapshotID is the full restic snapshot id (or unique prefix accepted by Backrest).
	SnapshotID string `json:"snapshotID"`
	// Path inside the snapshot to stream as .tar (default "/").
	Path string `json:"path,omitempty"`
	// PlanID optionally narrows GetOperations to a Backrest plan.
	PlanID string `json:"planID,omitempty"`
	// PublicBaseURL overrides BACKREST_PUBLIC_BASE_URL for the absolute download link.
	PublicBaseURL string `json:"publicBaseURL,omitempty"`
	// Mode controls how the URL is minted:
	// - restore (default): schedule Backrest Restore (visible in UI), then GetDownloadURL
	// - stream: GetDownloadURL from indexed snapshot only (no Restore op in UI)
	Mode string `json:"mode,omitempty"`
}

// SnapshotDownloadStatus holds the minted download URL.
type SnapshotDownloadStatus struct {
	Phase         string      `json:"phase,omitempty"`
	DownloadURL   string      `json:"downloadURL,omitempty"`
	RelativeURL   string      `json:"relativeURL,omitempty"`
	OperationID   int64       `json:"operationID,omitempty"`
	SnapshotID    string      `json:"snapshotID,omitempty"`
	Path          string      `json:"path,omitempty"`
	ExpiresAt     string      `json:"expiresAt,omitempty"`
	LastRefresh   string      `json:"lastRefresh,omitempty"`
	Message       string      `json:"message,omitempty"`
	Conditions    []Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type SnapshotDownload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SnapshotDownloadSpec   `json:"spec,omitempty"`
	Status            SnapshotDownloadStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SnapshotDownloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SnapshotDownload `json:"items"`
}
