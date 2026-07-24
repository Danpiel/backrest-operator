"""MCP tool implementations using impersonated/custom API access."""

from __future__ import annotations

import logging
from typing import Any

from shared import k8sutil
from shared.constants import (
    ANNOTATION_ALLOW_DESTRUCTIVE,
    API_GROUP,
    API_VERSION,
    PLURAL_CLUSTER,
    PLURAL_PLAN,
    PLURAL_PVCBACKUP,
    PLURAL_PVCRESTORE,
    PLURAL_REPOSITORY,
)

log = logging.getLogger(__name__)


def _api():
    return k8sutil.custom()


def list_clusters(namespace: str) -> list[dict[str, Any]]:
    if namespace:
        res = _api().list_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_CLUSTER)
    else:
        res = _api().list_cluster_custom_object(API_GROUP, API_VERSION, PLURAL_CLUSTER)
    return res.get("items") or []


def get_cluster(namespace: str, name: str) -> dict[str, Any]:
    return _api().get_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_CLUSTER, name)


def list_repositories(namespace: str) -> list[dict[str, Any]]:
    res = _api().list_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_REPOSITORY)
    return res.get("items") or []


def get_repository(namespace: str, name: str) -> dict[str, Any]:
    return _api().get_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_REPOSITORY, name)


def create_repository(namespace: str, body: dict[str, Any]) -> dict[str, Any]:
    body.setdefault("apiVersion", f"{API_GROUP}/{API_VERSION}")
    body.setdefault("kind", "BackupRepository")
    body.setdefault("metadata", {})["namespace"] = namespace
    return _api().create_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_REPOSITORY, body)


def delete_repository(namespace: str, name: str, *, allow_destructive: bool) -> dict[str, str]:
    if not allow_destructive:
        raise PermissionError("allow_destructive=true required")
    _api().delete_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_REPOSITORY, name)
    return {"deleted": name}


def list_plans(namespace: str) -> list[dict[str, Any]]:
    res = _api().list_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PLAN)
    return res.get("items") or []


def create_plan(namespace: str, body: dict[str, Any]) -> dict[str, Any]:
    body.setdefault("apiVersion", f"{API_GROUP}/{API_VERSION}")
    body.setdefault("kind", "BackupPlan")
    body.setdefault("metadata", {})["namespace"] = namespace
    return _api().create_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PLAN, body)


def delete_plan(namespace: str, name: str, *, allow_destructive: bool) -> dict[str, str]:
    if not allow_destructive:
        raise PermissionError("allow_destructive=true required")
    _api().delete_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PLAN, name)
    return {"deleted": name}


def trigger_backup(namespace: str, body: dict[str, Any]) -> dict[str, Any]:
    body.setdefault("apiVersion", f"{API_GROUP}/{API_VERSION}")
    body.setdefault("kind", "PVCBackup")
    body.setdefault("metadata", {})["namespace"] = namespace
    return _api().create_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PVCBACKUP, body)


def create_pvc_restore(namespace: str, body: dict[str, Any]) -> dict[str, Any]:
    body.setdefault("apiVersion", f"{API_GROUP}/{API_VERSION}")
    body.setdefault("kind", "PVCRestore")
    body.setdefault("metadata", {})["namespace"] = namespace
    return _api().create_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PVCRESTORE, body)


def restore_export(
    namespace: str,
    *,
    repository_name: str,
    repository_namespace: str | None = None,
    snapshot_id: str = "latest",
    path_filters: list[str] | None = None,
    ttl_seconds: int = 3600,
) -> dict[str, Any]:
    body = {
        "apiVersion": f"{API_GROUP}/{API_VERSION}",
        "kind": "PVCRestore",
        "metadata": {"generateName": "export-", "namespace": namespace},
        "spec": {
            "mode": "export",
            "repositoryRef": {
                "name": repository_name,
                "namespace": repository_namespace or namespace,
            },
            "restic": {"snapshotID": snapshot_id, "pathFilters": path_filters or []},
            "export": {"enabled": True, "ttlSeconds": ttl_seconds, "oneShot": True, "format": "tar"},
        },
    }
    return _api().create_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PVCRESTORE, body)


def get_pvc_restore(namespace: str, name: str) -> dict[str, Any]:
    return _api().get_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PVCRESTORE, name)


def repo_status(namespace: str, name: str) -> dict[str, Any]:
    obj = get_repository(namespace, name)
    return {"metadata": obj.get("metadata"), "status": obj.get("status"), "spec": {"url": (obj.get("spec") or {}).get("url"), "appendOnly": (obj.get("spec") or {}).get("appendOnly")}}
