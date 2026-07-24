"""PVC restore and export orchestration."""

from __future__ import annotations

import logging
import secrets
import time
from typing import Any

from kubernetes.client import ApiException

from backrest_operator.workflows import quiesce as quiesce_wf
from backrest_operator.workflows import restic_job
from shared import k8sutil
from shared.constants import API_GROUP, API_VERSION, PLURAL_REPOSITORY

log = logging.getLogger(__name__)


def _get_repo(ref: dict[str, Any], default_ns: str) -> dict[str, Any]:
    return k8sutil.custom().get_namespaced_custom_object(
        API_GROUP, API_VERSION, ref.get("namespace") or default_ns, PLURAL_REPOSITORY, ref["name"]
    )


def run_pvc_restore(restore: dict[str, Any]) -> dict[str, Any]:
    meta = restore["metadata"]
    name, ns = meta["name"], meta["namespace"]
    spec = restore.get("spec") or {}
    mode = spec.get("mode") or "fromResticToExistingPVC"
    quiesce_state = None
    try:
        if mode == "fromVolumeSnapshot":
            return _restore_from_snapshot(restore)
        if mode == "export":
            return _restore_export(restore)

        repo = _get_repo(spec.get("repositoryRef") or {}, ns)
        restic = spec.get("restic") or {}
        snapshot_id = restic.get("snapshotID") or "latest"
        path_filters = restic.get("pathFilters") or []
        target = spec.get("target") or {}

        if mode == "fromResticToNewPVC":
            new_pvc = target.get("newPVC") or {}
            pvc_name = new_pvc.get("name") or f"{name}-pvc"
            _create_empty_pvc(ns, new_pvc, pvc_name)
        else:
            pvc_name = target.get("existingPVCName")
            if not pvc_name:
                raise ValueError("target.existingPVCName required")
            q = spec.get("quiesce") or {}
            if q.get("enabled", True):
                quiesce_state = quiesce_wf.quiesce(
                    q.get("targets") or [],
                    default_namespace=ns,
                    timeout_seconds=int(q.get("timeoutSeconds") or 900),
                )

        includes = []
        for p in path_filters:
            includes.extend(["--include", p])
        job_name = f"pvcrestore-{name}-{int(time.time())}"[:63]
        cmd = ["restic", "restore", snapshot_id, "--target", "/data"] + includes
        body = restic_job.build_restic_job(
            name=job_name,
            namespace=ns,
            repo=repo,
            command=cmd,
            pvc_name=pvc_name,
            labels=k8sutil.labels(name, "restore-job"),
        )
        restic_job.create_or_get_job(body)
        result = restic_job.wait_job(job_name, ns)
        if result != "Succeeded":
            raise RuntimeError(f"restore job {result}")
        return {"phase": "Succeeded", "lastJobName": job_name, "conditions": []}
    except Exception as e:
        log.exception("restore failed")
        return {"phase": "Failed", "conditions": [{"type": "Failed", "status": "True", "message": str(e)}]}
    finally:
        if quiesce_state:
            try:
                quiesce_wf.unquiesce(quiesce_state)
            except Exception:
                log.exception("unquiesce failed")


def _create_empty_pvc(ns: str, new_pvc: dict[str, Any], pvc_name: str) -> None:
    body = {
        "apiVersion": "v1",
        "kind": "PersistentVolumeClaim",
        "metadata": {"name": pvc_name, "namespace": ns},
        "spec": {
            "accessModes": new_pvc.get("accessModes") or ["ReadWriteOnce"],
            "resources": {"requests": {"storage": new_pvc.get("size") or "10Gi"}},
        },
    }
    if new_pvc.get("storageClassName"):
        body["spec"]["storageClassName"] = new_pvc["storageClassName"]
    try:
        k8sutil.core().create_namespaced_persistent_volume_claim(ns, body)
    except ApiException as e:
        if e.status != 409:
            raise


def _restore_from_snapshot(restore: dict[str, Any]) -> dict[str, Any]:
    meta = restore["metadata"]
    ns = meta["namespace"]
    spec = restore.get("spec") or {}
    ref = spec.get("volumeSnapshotRef") or {}
    target = (spec.get("target") or {}).get("newPVC") or {}
    pvc_name = target.get("name") or f"{meta['name']}-pvc"
    body = {
        "apiVersion": "v1",
        "kind": "PersistentVolumeClaim",
        "metadata": {"name": pvc_name, "namespace": ns},
        "spec": {
            "accessModes": target.get("accessModes") or ["ReadWriteOnce"],
            "resources": {"requests": {"storage": target.get("size") or "10Gi"}},
            "dataSource": {
                "name": ref.get("name"),
                "kind": "VolumeSnapshot",
                "apiGroup": "snapshot.storage.k8s.io",
            },
        },
    }
    if target.get("storageClassName"):
        body["spec"]["storageClassName"] = target["storageClassName"]
    try:
        k8sutil.core().create_namespaced_persistent_volume_claim(ns, body)
    except ApiException as e:
        if e.status != 409:
            raise
    return {"phase": "Succeeded", "conditions": []}


def _restore_export(restore: dict[str, Any]) -> dict[str, Any]:
    meta = restore["metadata"]
    name, ns = meta["name"], meta["namespace"]
    spec = restore.get("spec") or {}
    export = spec.get("export") or {}
    repo = _get_repo(spec.get("repositoryRef") or {}, ns)
    restic = spec.get("restic") or {}
    token = secrets.token_urlsafe(24)
    ttl = int(export.get("ttlSeconds") or 3600)
    job_name = f"export-{name}-{int(time.time())}"[:63]
    labels = k8sutil.labels(name, "export")
    # selector labels for service must match pod template labels
    body = restic_job.build_export_proxy_job(
        name=job_name,
        namespace=ns,
        repo=repo,
        snapshot_id=restic.get("snapshotID") or "latest",
        path_filters=restic.get("pathFilters") or [],
        token=token,
        ttl_seconds=ttl,
        labels=labels,
    )
    restic_job.create_or_get_job(body)
    svc_name = f"export-{name}"[:63]
    base = restic_job.ensure_export_service(svc_name, ns, job_name, labels)
    url = f"{base}/{token}/archive.tar"
    expires = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + ttl))
    return {
        "phase": "Succeeded",
        "exportURL": url,
        "exportExpiresAt": expires,
        "lastJobName": job_name,
        "conditions": [],
    }
