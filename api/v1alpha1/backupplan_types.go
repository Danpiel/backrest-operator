package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type BackupPlanSpec struct {
	RepositoryRef ObjectReference         `json:"repositoryRef"`
	ClusterRef    ObjectReference         `json:"clusterRef,omitempty"`
	Schedule      string                 `json:"schedule,omitempty"`
	Paths         []string               `json:"paths,omitempty"`
	Excludes      []string               `json:"excludes,omitempty"`
	Retention     map[string]interface{} `json:"retention,omitempty"`
	Hooks         []map[string]interface{} `json:"hooks,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	PVCBackupRef  ObjectReference         `json:"pvcBackupRef,omitempty"`
}

type BackupPlanStatus struct {
	Phase          string      `json:"phase,omitempty"`
	LastBackupTime string      `json:"lastBackupTime,omitempty"`
	LastSnapshotID string      `json:"lastSnapshotID,omitempty"`
	Conditions     []Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type BackupPlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              BackupPlanSpec   `json:"spec,omitempty"`
	Status            BackupPlanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupPlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupPlan `json:"items"`
}
