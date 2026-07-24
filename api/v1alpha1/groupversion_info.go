package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	Group   = "operator.backrest.io"
	Version = "v1alpha1"
)

var (
	GroupVersion  = schema.GroupVersion{Group: Group, Version: Version}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(
		&BackrestCluster{}, &BackrestClusterList{},
		&BackupRepository{}, &BackupRepositoryList{},
		&BackupPlan{}, &BackupPlanList{},
		&PVCBackup{}, &PVCBackupList{},
		&PVCRestore{}, &PVCRestoreList{},
	)
}

// Condition is a generic status condition.
type Condition struct {
	Type    string `json:"type,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// LocalObjectReference is a name-only ref.
type LocalObjectReference struct {
	Name string `json:"name"`
}

// ObjectReference with optional namespace.
type ObjectReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// SecretKeySelector selects a key in a Secret.
type SecretKeySelector struct {
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}

// Common object meta helpers for lists.
type TypeMeta = metav1.TypeMeta
type ObjectMeta = metav1.ObjectMeta
type ListMeta = metav1.ListMeta
