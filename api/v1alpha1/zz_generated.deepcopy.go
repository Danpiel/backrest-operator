package v1alpha1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func jsonCopy(dst, src interface{}) {
	b, err := json.Marshal(src)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		panic(err)
	}
}

func (in *BackrestCluster) DeepCopyInto(out *BackrestCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	jsonCopy(&out.Spec, &in.Spec)
	jsonCopy(&out.Status, &in.Status)
}
func (in *BackrestCluster) DeepCopy() *BackrestCluster {
	if in == nil {
		return nil
	}
	out := new(BackrestCluster)
	in.DeepCopyInto(out)
	return out
}
func (in *BackrestCluster) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
func (in *BackrestClusterList) DeepCopyInto(out *BackrestClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]BackrestCluster, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
func (in *BackrestClusterList) DeepCopy() *BackrestClusterList {
	if in == nil {
		return nil
	}
	out := new(BackrestClusterList)
	in.DeepCopyInto(out)
	return out
}
func (in *BackrestClusterList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *BackupRepository) DeepCopyInto(out *BackupRepository) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	jsonCopy(&out.Spec, &in.Spec)
	jsonCopy(&out.Status, &in.Status)
}
func (in *BackupRepository) DeepCopy() *BackupRepository {
	if in == nil {
		return nil
	}
	out := new(BackupRepository)
	in.DeepCopyInto(out)
	return out
}
func (in *BackupRepository) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
func (in *BackupRepositoryList) DeepCopyInto(out *BackupRepositoryList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]BackupRepository, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
func (in *BackupRepositoryList) DeepCopy() *BackupRepositoryList {
	if in == nil {
		return nil
	}
	out := new(BackupRepositoryList)
	in.DeepCopyInto(out)
	return out
}
func (in *BackupRepositoryList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *BackupPlan) DeepCopyInto(out *BackupPlan) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	jsonCopy(&out.Spec, &in.Spec)
	jsonCopy(&out.Status, &in.Status)
}
func (in *BackupPlan) DeepCopy() *BackupPlan {
	if in == nil {
		return nil
	}
	out := new(BackupPlan)
	in.DeepCopyInto(out)
	return out
}
func (in *BackupPlan) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
func (in *BackupPlanList) DeepCopyInto(out *BackupPlanList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]BackupPlan, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
func (in *BackupPlanList) DeepCopy() *BackupPlanList {
	if in == nil {
		return nil
	}
	out := new(BackupPlanList)
	in.DeepCopyInto(out)
	return out
}
func (in *BackupPlanList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *PVCBackup) DeepCopyInto(out *PVCBackup) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	jsonCopy(&out.Spec, &in.Spec)
	jsonCopy(&out.Status, &in.Status)
}
func (in *PVCBackup) DeepCopy() *PVCBackup {
	if in == nil {
		return nil
	}
	out := new(PVCBackup)
	in.DeepCopyInto(out)
	return out
}
func (in *PVCBackup) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
func (in *PVCBackupList) DeepCopyInto(out *PVCBackupList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]PVCBackup, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
func (in *PVCBackupList) DeepCopy() *PVCBackupList {
	if in == nil {
		return nil
	}
	out := new(PVCBackupList)
	in.DeepCopyInto(out)
	return out
}
func (in *PVCBackupList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *PVCRestore) DeepCopyInto(out *PVCRestore) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	jsonCopy(&out.Spec, &in.Spec)
	jsonCopy(&out.Status, &in.Status)
}
func (in *PVCRestore) DeepCopy() *PVCRestore {
	if in == nil {
		return nil
	}
	out := new(PVCRestore)
	in.DeepCopyInto(out)
	return out
}
func (in *PVCRestore) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
func (in *PVCRestoreList) DeepCopyInto(out *PVCRestoreList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]PVCRestore, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
func (in *PVCRestoreList) DeepCopy() *PVCRestoreList {
	if in == nil {
		return nil
	}
	out := new(PVCRestoreList)
	in.DeepCopyInto(out)
	return out
}
func (in *PVCRestoreList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// Ensure metav1 import used when compiling older toolchains.
var _ = metav1.Time{}
