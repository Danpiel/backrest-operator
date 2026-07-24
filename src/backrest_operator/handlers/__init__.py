"""Kopf handlers for all CRDs."""

from __future__ import annotations

import logging

import kopf

from backrest_operator.workflows import cluster as cluster_wf
from backrest_operator.workflows import pvcbackup as pvcbackup_wf
from backrest_operator.workflows import pvcrestore as pvcrestore_wf
from backrest_operator.workflows import repository as repo_wf
from shared.constants import (
    API_GROUP,
    KIND_CLUSTER,
    KIND_PLAN,
    KIND_PVCBACKUP,
    KIND_PVCRESTORE,
    KIND_REPOSITORY,
    PLURAL_CLUSTER,
    PLURAL_PLAN,
    PLURAL_PVCBACKUP,
    PLURAL_PVCRESTORE,
    PLURAL_REPOSITORY,
)
from shared.filters import namespace_allowed
from shared.metrics import RECONCILE_ERRORS
from shared import k8sutil

log = logging.getLogger(__name__)


def _gate(namespace: str, meta: dict) -> bool:
    if not namespace_allowed(namespace):
        return False
    selector = __import__("shared.filters", fromlist=["label_selector"]).label_selector()
    if not selector:
        return True
    # Simple equality selector parsing: k=v,k2=v2
    labels = meta.get("labels") or {}
    for part in selector.split(","):
        if not part.strip():
            continue
        if "=" not in part:
            continue
        k, v = part.split("=", 1)
        if labels.get(k.strip()) != v.strip():
            return False
    return True


@kopf.on.create(API_GROUP, "v1alpha1", PLURAL_CLUSTER)
@kopf.on.update(API_GROUP, "v1alpha1", PLURAL_CLUSTER)
@kopf.on.resume(API_GROUP, "v1alpha1", PLURAL_CLUSTER)
def on_cluster(spec, name, namespace, meta, body, status, patch, **_):
    if not _gate(namespace, dict(meta)):
        raise kopf.TemporaryError("filtered out", delay=300)
    try:
        st = cluster_wf.ensure_cluster(dict(body))
        patch.status.update(st)
    except Exception as e:
        RECONCILE_ERRORS.labels(KIND_CLUSTER).inc()
        log.exception("cluster reconcile")
        patch.status["phase"] = "Failed"
        raise kopf.TemporaryError(str(e), delay=30)


@kopf.on.create(API_GROUP, "v1alpha1", PLURAL_REPOSITORY)
@kopf.on.update(API_GROUP, "v1alpha1", PLURAL_REPOSITORY)
@kopf.on.resume(API_GROUP, "v1alpha1", PLURAL_REPOSITORY)
def on_repository(spec, name, namespace, meta, body, patch, **_):
    if not _gate(namespace, dict(meta)):
        raise kopf.TemporaryError("filtered out", delay=300)
    try:
        st = repo_wf.reconcile_repository(dict(body))
        patch.status.update(st)
    except Exception as e:
        RECONCILE_ERRORS.labels(KIND_REPOSITORY).inc()
        raise kopf.TemporaryError(str(e), delay=30)


@kopf.on.create(API_GROUP, "v1alpha1", PLURAL_PLAN)
@kopf.on.update(API_GROUP, "v1alpha1", PLURAL_PLAN)
@kopf.on.resume(API_GROUP, "v1alpha1", PLURAL_PLAN)
def on_plan(spec, name, namespace, meta, body, patch, **_):
    if not _gate(namespace, dict(meta)):
        raise kopf.TemporaryError("filtered out", delay=300)
    try:
        st = repo_wf.reconcile_plan(dict(body))
        patch.status.update(st)
    except Exception as e:
        RECONCILE_ERRORS.labels(KIND_PLAN).inc()
        raise kopf.TemporaryError(str(e), delay=30)


@kopf.on.create(API_GROUP, "v1alpha1", PLURAL_PVCBACKUP)
@kopf.on.resume(API_GROUP, "v1alpha1", PLURAL_PVCBACKUP)
def on_pvcbackup(spec, name, namespace, meta, body, status, patch, **_):
    if not _gate(namespace, dict(meta)):
        raise kopf.TemporaryError("filtered out", delay=300)
    # Skip if already succeeded one-shot without schedule
    if (status or {}).get("phase") == "Succeeded" and not (spec or {}).get("schedule"):
        return
    try:
        patch.status["phase"] = "Pending"
        st = pvcbackup_wf.run_pvc_backup(dict(body))
        patch.status.update(st)
        if st.get("phase") == "Failed":
            raise kopf.TemporaryError(st.get("conditions", [{}])[0].get("message", "failed"), delay=60)
    except kopf.TemporaryError:
        raise
    except Exception as e:
        RECONCILE_ERRORS.labels(KIND_PVCBACKUP).inc()
        patch.status["phase"] = "Failed"
        raise kopf.TemporaryError(str(e), delay=60)


@kopf.on.create(API_GROUP, "v1alpha1", PLURAL_PVCRESTORE)
@kopf.on.resume(API_GROUP, "v1alpha1", PLURAL_PVCRESTORE)
def on_pvcrestore(spec, name, namespace, meta, body, status, patch, **_):
    if not _gate(namespace, dict(meta)):
        raise kopf.TemporaryError("filtered out", delay=300)
    if (status or {}).get("phase") == "Succeeded":
        return
    try:
        st = pvcrestore_wf.run_pvc_restore(dict(body))
        patch.status.update(st)
        if st.get("phase") == "Failed":
            raise kopf.TemporaryError(st.get("conditions", [{}])[0].get("message", "failed"), delay=60)
    except kopf.TemporaryError:
        raise
    except Exception as e:
        RECONCILE_ERRORS.labels(KIND_PVCRESTORE).inc()
        raise kopf.TemporaryError(str(e), delay=60)


# Timer for scheduled PVCBackups with cron — lightweight check
@kopf.timer(API_GROUP, "v1alpha1", PLURAL_PVCBACKUP, interval=60.0)
def pvcbackup_schedule(spec, name, namespace, meta, body, status, patch, **_):
    if not _gate(namespace, dict(meta)):
        return
    schedule = (spec or {}).get("schedule") or ""
    if not schedule:
        return
    # Cron evaluation: trigger if last success older than coarse window — full cron in CronJob preferred
    # Create companion CronJob when schedule set
    _ensure_backup_cronjob(dict(body))


def _ensure_backup_cronjob(backup: dict) -> None:
    from kubernetes.client import ApiException

    meta = backup["metadata"]
    name, ns = meta["name"], meta["namespace"]
    schedule = (backup.get("spec") or {}).get("schedule")
    if not schedule:
        return
    # Annotate CR for CronJob to patch a trigger annotation — simplified: CronJob applies same pipeline via Job calling kubectl is heavy.
    # Instead create a CronJob that sets annotation to force reconcile.
    cron_name = f"pvcbackup-{name}"[:52]
    job = {
        "apiVersion": "batch/v1",
        "kind": "CronJob",
        "metadata": {"name": cron_name, "namespace": ns},
        "spec": {
            "schedule": schedule,
            "concurrencyPolicy": "Forbid",
            "jobTemplate": {
                "spec": {
                    "template": {
                        "spec": {
                            "restartPolicy": "OnFailure",
                            "serviceAccountName": "backrest-operator",
                            "containers": [
                                {
                                    "name": "trigger",
                                    "image": "bitnami/kubectl:latest",
                                    "command": [
                                        "kubectl",
                                        "annotate",
                                        "pvcbackup",
                                        name,
                                        f"{API_GROUP}/trigger=$(date +%s)",
                                        "--overwrite",
                                    ],
                                }
                            ],
                        }
                    }
                }
            },
        },
    }
    try:
        k8sutil.batch().create_namespaced_cron_job(ns, job)
    except ApiException as e:
        if e.status != 409:
            log.warning("cronjob ensure: %s", e)
