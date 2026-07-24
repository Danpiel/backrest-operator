"""BackupRepository and BackupPlan helpers."""

from __future__ import annotations

import json
import logging
from typing import Any

from kubernetes.client import ApiException

from backrest_operator.workflows import restic_job
from shared import k8sutil
from shared.constants import API_GROUP, API_VERSION, PLURAL_PLAN

log = logging.getLogger(__name__)


def reconcile_repository(repo: dict[str, Any]) -> dict[str, Any]:
    spec = repo.get("spec") or {}
    url = spec.get("url") or ""
    if not url:
        return {"phase": "Failed", "conditions": [{"type": "Invalid", "status": "True", "message": "url required"}]}
    pref = spec.get("passwordSecretRef") or {}
    if not pref.get("name"):
        return {"phase": "Failed", "conditions": [{"type": "Invalid", "status": "True", "message": "passwordSecretRef required"}]}

    verify = spec.get("verify") or {}
    # Default verify enabled
    verify_enabled = verify.get("enabled", True)
    status = {
        "phase": "Ready",
        "resticURL": url,
        "lastCheckTime": "",
        "lastCheckResult": "skipped" if not verify_enabled else "scheduled",
        "conditions": [],
    }
    if verify_enabled:
        _ensure_check_cronjob(repo)
    return status


def _ensure_check_cronjob(repo: dict[str, Any]) -> None:
    meta = repo["metadata"]
    name, ns = meta["name"], meta["namespace"]
    schedule = ((repo.get("spec") or {}).get("verify") or {}).get("schedule") or "0 3 * * 0"
    append_only = bool((repo.get("spec") or {}).get("appendOnly"))
    job_template = restic_job.build_restic_job(
        name=f"check-{name}",
        namespace=ns,
        repo=repo,
        command=["restic", "check"],
        labels=k8sutil.labels(name, "repo-check"),
        append_only=append_only,
        ttl_seconds=3600,
    )
    cron = {
        "apiVersion": "batch/v1",
        "kind": "CronJob",
        "metadata": {"name": f"restic-check-{name}", "namespace": ns},
        "spec": {
            "schedule": schedule,
            "concurrencyPolicy": "Forbid",
            "jobTemplate": {"spec": job_template["spec"]},
        },
    }
    batch = k8sutil.batch()
    try:
        batch.create_namespaced_cron_job(ns, cron)
    except ApiException as e:
        if e.status == 409:
            batch.patch_namespaced_cron_job(f"restic-check-{name}", ns, cron)
        else:
            raise


def plan_to_backrest_fragment(plan: dict[str, Any]) -> dict[str, Any]:
    """Serialize BackupPlan into a Backrest-compatible plan fragment (best-effort)."""
    meta = plan.get("metadata") or {}
    spec = plan.get("spec") or {}
    retention = spec.get("retention") or {}
    return {
        "id": f"{meta.get('namespace')}-{meta.get('name')}",
        "repo": ((spec.get("repositoryRef") or {}).get("name")),
        "paths": spec.get("paths") or [],
        "excludes": spec.get("excludes") or [],
        "schedule": spec.get("schedule") or "",
        "retention": retention,
        "hooks": spec.get("hooks") or [],
        "tags": spec.get("tags") or [],
    }


def reconcile_plan(plan: dict[str, Any]) -> dict[str, Any]:
    meta = plan["metadata"]
    ns = meta["namespace"]
    fragment = plan_to_backrest_fragment(plan)
    cm_name = "backrest-plans"
    core = k8sutil.core()
    key = f"{meta['namespace']}.{meta['name']}.json"
    data_value = json.dumps(fragment, indent=2)
    try:
        cm = core.read_namespaced_config_map(cm_name, ns)
        data = dict(cm.data or {})
        data[key] = data_value
        core.patch_namespaced_config_map(cm_name, ns, {"data": data})
    except ApiException as e:
        if e.status == 404:
            # Prefer cluster namespace from clusterRef
            cref = (plan.get("spec") or {}).get("clusterRef") or {}
            target_ns = cref.get("namespace") or ns
            body = {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "metadata": {"name": cm_name, "namespace": target_ns},
                "data": {key: data_value},
            }
            try:
                if target_ns != ns:
                    try:
                        existing = core.read_namespaced_config_map(cm_name, target_ns)
                        data = dict(existing.data or {})
                        data[key] = data_value
                        core.patch_namespaced_config_map(cm_name, target_ns, {"data": data})
                    except ApiException as e2:
                        if e2.status == 404:
                            core.create_namespaced_config_map(target_ns, body)
                        else:
                            raise
                else:
                    core.create_namespaced_config_map(ns, body)
            except ApiException as e3:
                if e3.status != 409:
                    raise
        else:
            raise
    return {"phase": "Ready", "conditions": []}


def collect_plans(namespace: str | None = None) -> list[dict[str, Any]]:
    api = k8sutil.custom()
    if namespace:
        res = api.list_namespaced_custom_object(API_GROUP, API_VERSION, namespace, PLURAL_PLAN)
    else:
        res = api.list_cluster_custom_object(API_GROUP, API_VERSION, PLURAL_PLAN)
    return list(res.get("items") or [])
