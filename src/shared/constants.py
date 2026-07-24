"""Shared constants for Backrest Operator."""

API_GROUP = "operator.backrest.io"
API_VERSION = "v1alpha1"
API = f"{API_GROUP}/{API_VERSION}"

KIND_CLUSTER = "BackrestCluster"
KIND_REPOSITORY = "BackupRepository"
KIND_PLAN = "BackupPlan"
KIND_PVCBACKUP = "PVCBackup"
KIND_PVCRESTORE = "PVCRestore"

PLURAL_CLUSTER = "backrestclusters"
PLURAL_REPOSITORY = "backuprepositories"
PLURAL_PLAN = "backupplans"
PLURAL_PVCBACKUP = "pvcbackups"
PLURAL_PVCRESTORE = "pvcrestores"

LABEL_MANAGED_BY = "app.kubernetes.io/managed-by"
LABEL_PART_OF = "app.kubernetes.io/part-of"
LABEL_COMPONENT = "app.kubernetes.io/component"
LABEL_INSTANCE = "app.kubernetes.io/instance"
LABEL_CLUSTER = f"{API_GROUP}/cluster"
LABEL_ROLE = f"{API_GROUP}/role"

MANAGED_BY = "backrest-operator"
PART_OF = "backrest-operator"

DEFAULT_BACKREST_IMAGE = "ghcr.io/garethgeorge/backrest"
DEFAULT_BACKREST_TAG = "v1.14.1"
BACKREST_PORT = 9898

ANNOTATION_ALLOW_DESTRUCTIVE = f"{API_GROUP}/allow-destructive"
ANNOTATION_LEAVE_DOWN = f"{API_GROUP}/leave-down-confirmed"

RESTIC_IMAGE = "restic/restic:0.19.1"
