package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type BackupRepositorySpec struct {
	URL               string             `json:"url"`
	PasswordSecretRef SecretKeySelector  `json:"passwordSecretRef"`
	EnvFromSecretRef  *LocalObjectReference `json:"envFromSecretRef,omitempty"`
	AppendOnly        bool               `json:"appendOnly,omitempty"`
	Verify            RepoVerifySpec     `json:"verify,omitempty"`
	Shared            bool               `json:"shared,omitempty"`
	Backrest          RepoBackrestSpec   `json:"backrest,omitempty"`
}

type RepoVerifySpec struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Schedule string `json:"schedule,omitempty"`
}

type RepoBackrestSpec struct {
	SyncToHost bool            `json:"syncToHost,omitempty"`
	ClusterRef ObjectReference `json:"clusterRef,omitempty"`
}

type BackupRepositoryStatus struct {
	Phase           string      `json:"phase,omitempty"`
	ResticURL       string      `json:"resticURL,omitempty"`
	LastCheckTime   string      `json:"lastCheckTime,omitempty"`
	LastCheckResult string      `json:"lastCheckResult,omitempty"`
	Conditions      []Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type BackupRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              BackupRepositorySpec   `json:"spec,omitempty"`
	Status            BackupRepositoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupRepository `json:"items"`
}
