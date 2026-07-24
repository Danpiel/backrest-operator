package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type PVCRestoreSpec struct {
	Mode              string            `json:"mode"`
	RepositoryRef     ObjectReference   `json:"repositoryRef,omitempty"`
	Restic            ResticRestoreSpec `json:"restic,omitempty"`
	VolumeSnapshotRef ObjectReference   `json:"volumeSnapshotRef,omitempty"`
	Target            RestoreTargetSpec `json:"target,omitempty"`
	Quiesce           QuiesceSpec       `json:"quiesce,omitempty"`
	Export            ExportSpec        `json:"export,omitempty"`
}

type ResticRestoreSpec struct {
	SnapshotID  string   `json:"snapshotID,omitempty"`
	PathFilters []string `json:"pathFilters,omitempty"`
}

type RestoreTargetSpec struct {
	ExistingPVCName string      `json:"existingPVCName,omitempty"`
	NewPVC          NewPVCSpec  `json:"newPVC,omitempty"`
}

type NewPVCSpec struct {
	Name             string   `json:"name,omitempty"`
	Size             string   `json:"size,omitempty"`
	StorageClassName string   `json:"storageClassName,omitempty"`
	AccessModes      []string `json:"accessModes,omitempty"`
}

type ExportSpec struct {
	Enabled    bool   `json:"enabled,omitempty"`
	TTLSeconds int32  `json:"ttlSeconds,omitempty"`
	OneShot    bool   `json:"oneShot,omitempty"`
	Format     string `json:"format,omitempty"`
}

type PVCRestoreStatus struct {
	Phase           string      `json:"phase,omitempty"`
	ExportURL       string      `json:"exportURL,omitempty"`
	ExportExpiresAt string      `json:"exportExpiresAt,omitempty"`
	LastJobName     string      `json:"lastJobName,omitempty"`
	Conditions      []Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type PVCRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PVCRestoreSpec   `json:"spec,omitempty"`
	Status            PVCRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type PVCRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PVCRestore `json:"items"`
}
