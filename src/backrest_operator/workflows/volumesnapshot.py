"""CSI / TopoLVM VolumeSnapshot helpers."""

from __future__ import annotations

import logging
import time
from typing import Any

from kubernetes.client import ApiException

from shared import k8sutil

log = logging.getLogger(__name__)

SNAP_GROUP = "snapshot.storage.k8s.io"
SNAP_VERSION = "v1"
SNAP_PLURAL = "volumesnapshots"


def create_volume_snapshot(
    *,
    name: str,
    namespace: str,
    pvc_name: str,
    volume_snapshot_class: str,
    labels: dict[str, str] | None = None,
) -> dict[str, Any]:
    body = {
        "apiVersion": f"{SNAP_GROUP}/{SNAP_VERSION}",
        "kind": "VolumeSnapshot",
        "metadata": {"name": name, "namespace": namespace, "labels": labels or {}},
        "spec": {
            "volumeSnapshotClassName": volume_snapshot_class,
            "source": {"persistentVolumeClaimName": pvc_name},
        },
    }
    api = k8sutil.custom()
    try:
        return api.create_namespaced_custom_object(SNAP_GROUP, SNAP_VERSION, namespace, SNAP_PLURAL, body)
    except ApiException as e:
        if e.status == 409:
            return api.get_namespaced_custom_object(SNAP_GROUP, SNAP_VERSION, namespace, SNAP_PLURAL, name)
        raise


def wait_snapshot_ready(name: str, namespace: str, timeout: int = 600) -> dict[str, Any]:
    api = k8sutil.custom()
    deadline = time.time() + timeout
    while time.time() < deadline:
        obj = api.get_namespaced_custom_object(SNAP_GROUP, SNAP_VERSION, namespace, SNAP_PLURAL, name)
        status = obj.get("status") or {}
        if status.get("readyToUse") is True:
            return obj
        err = status.get("error") or {}
        if err.get("message"):
            raise RuntimeError(f"VolumeSnapshot {namespace}/{name} error: {err['message']}")
        time.sleep(3)
    raise TimeoutError(f"VolumeSnapshot {namespace}/{name} not ready within {timeout}s")


def delete_volume_snapshot(name: str, namespace: str) -> None:
    api = k8sutil.custom()
    try:
        api.delete_namespaced_custom_object(SNAP_GROUP, SNAP_VERSION, namespace, SNAP_PLURAL, name)
    except ApiException as e:
        if e.status != 404:
            raise


def clone_pvc_from_snapshot(
    *,
    name: str,
    namespace: str,
    snapshot_name: str,
    storage_class: str | None,
    size: str,
    access_modes: list[str] | None = None,
) -> None:
    core = k8sutil.core()
    body = {
        "apiVersion": "v1",
        "kind": "PersistentVolumeClaim",
        "metadata": {"name": name, "namespace": namespace},
        "spec": {
            "accessModes": access_modes or ["ReadOnlyMany", "ReadWriteOnce"],
            "resources": {"requests": {"storage": size}},
            "dataSource": {
                "name": snapshot_name,
                "kind": "VolumeSnapshot",
                "apiGroup": SNAP_GROUP,
            },
        },
    }
    if storage_class:
        body["spec"]["storageClassName"] = storage_class
    # Prefer RWO if ROM not supported — use RWO only for broader CSI compatibility
    body["spec"]["accessModes"] = access_modes or ["ReadWriteOnce"]
    try:
        core.create_namespaced_persistent_volume_claim(namespace, body)
    except ApiException as e:
        if e.status != 409:
            raise
