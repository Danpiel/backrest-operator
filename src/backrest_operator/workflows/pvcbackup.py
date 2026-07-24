"""PVC backup orchestration pipeline."""

from __future__ import annotations

import logging
import time
from typing import Any

from backrest_operator.workflows import flush as flush_wf
from backrest_operator.workflows import quiesce as quiesce_wf
from backrest_operator.workflows import restic_job, volumesnapshot
from shared import k8sutil
from shared.constants import API_GROUP, API_VERSION, PLURAL_REPOSITORY
from shared.metrics import BACKUP_DURATION, BACKUP_LAST_SUCCESS, BACKUP_TOTAL

log = logging.getLogger(__name__)


def _get_repo(ref: dict[str, Any], default_ns: str) -> dict[str, Any]:
    name = ref.get("name")
    ns = ref.get("namespace") or default_ns
    return k8sutil.custom().get_namespaced_custom_object(
        API_GROUP, API_VERSION, ns, PLURAL_REPOSITORY, name
    )


def run_pvc_backup(backup: dict[str, Any]) -> dict[str, Any]:
    meta = backup["metadata"]
    name, ns = meta["name"], meta["namespace"]
    spec = backup.get("spec") or {}
    started = time.time()
    quiesce_state = None
    phase = "Pending"
    try:
        repo = _get_repo(spec.get("repositoryRef") or {}, ns)
        append_only = bool((repo.get("spec") or {}).get("appendOnly"))
        strategy = (spec.get("strategy") or {}).get("pipeline") or ["csiSnapshot"]
        pvc_name = spec.get("pvcName")
        if not pvc_name:
            raise ValueError("spec.pvcName is required")

        if "liveFlush" in strategy or ((spec.get("flush") or {}).get("enabled")):
            phase = "Flushing"
            flush_wf.run_flush(spec.get("flush"), namespace=ns)

        need_quiesce = "quiescedLive" in strategy or ((spec.get("quiesce") or {}).get("enabled"))
        if need_quiesce:
            phase = "Quiescing"
            q = spec.get("quiesce") or {}
            quiesce_state = quiesce_wf.quiesce(
                q.get("targets") or [],
                default_namespace=ns,
                timeout_seconds=int(q.get("timeoutSeconds") or 900),
            )

        snap_name = None
        upload_pvc = pvc_name
        node_name = None

        if "csiSnapshot" in strategy or "topolvmSnapshot" in strategy:
            phase = "Snapshotting"
            vsc = spec.get("volumeSnapshotClassName")
            if not vsc:
                raise ValueError("volumeSnapshotClassName required for snapshot strategies")
            snap_name = f"{name}-{int(started)}"
            volumesnapshot.create_volume_snapshot(
                name=snap_name,
                namespace=ns,
                pvc_name=pvc_name,
                volume_snapshot_class=vsc,
                labels=k8sutil.labels(name, "snapshot"),
            )
            volumesnapshot.wait_snapshot_ready(snap_name, ns)
            clone = f"{name}-clone-{int(started)}"
            # size from source PVC
            src = k8sutil.core().read_namespaced_persistent_volume_claim(pvc_name, ns)
            size = src.spec.resources.requests.get("storage", "10Gi")
            sc = src.spec.storage_class_name
            volumesnapshot.clone_pvc_from_snapshot(
                name=clone, namespace=ns, snapshot_name=snap_name, storage_class=sc, size=str(size)
            )
            upload_pvc = clone

        phase = "Uploading"
        paths = spec.get("paths") or ["/"]
        excludes = spec.get("excludes") or []
        cmd = ["restic", "backup"] + paths
        for ex in excludes:
            cmd.extend(["--exclude", ex])
        if append_only:
            # restic itself uses repo policy; keep env marker
            pass
        job_name = f"pvcbackup-{name}-{int(started)}"[:63]
        body = restic_job.build_restic_job(
            name=job_name,
            namespace=ns,
            repo=repo,
            command=cmd,
            pvc_name=upload_pvc,
            mount_path="/data",
            node_name=node_name,
            ttl_seconds=int(spec.get("ttlSecondsAfterFinished") or 86400),
            backoff_limit=int(spec.get("backoffLimit") or 2),
            labels=k8sutil.labels(name, "backup-job"),
            append_only=append_only,
        )
        # Adjust paths to mounted volume
        body["spec"]["template"]["spec"]["containers"][0]["command"] = (
            ["restic", "backup", "/data"]
            + [x for ex in excludes for x in ("--exclude", ex)]
        )
        restic_job.create_or_get_job(body)
        result = restic_job.wait_job(job_name, ns)
        if result != "Succeeded":
            raise RuntimeError(f"restic job {job_name} {result}")

        retention = spec.get("retention") or {}
        if snap_name and retention.get("deleteVolumeSnapshotAfterUpload", True):
            volumesnapshot.delete_volume_snapshot(snap_name, ns)

        duration = time.time() - started
        BACKUP_TOTAL.labels(ns, name, "success").inc()
        BACKUP_DURATION.labels(ns, name).observe(duration)
        BACKUP_LAST_SUCCESS.labels(ns, name).set(time.time())
        return {
            "phase": "Succeeded",
            "lastBackupTime": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "lastSnapshotName": snap_name or "",
            "lastResticSnapshotID": "",
            "lastJobName": job_name,
            "lastDurationSeconds": int(duration),
            "conditions": [],
        }
    except Exception as e:
        log.exception("pvc backup failed")
        BACKUP_TOTAL.labels(ns, name, "failure").inc()
        return {
            "phase": "Failed",
            "conditions": [{"type": "Failed", "status": "True", "message": str(e)}],
        }
    finally:
        leave_down = bool((spec.get("quiesce") or {}).get("leaveDown"))
        if quiesce_state and not leave_down:
            try:
                phase = "Unquiescing"
                quiesce_wf.unquiesce(quiesce_state)
            except Exception:
                log.exception("unquiesce failed")
