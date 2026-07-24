"""Quiesce / unquiesce workload helpers."""

from __future__ import annotations

import logging
import time
from typing import Any

from kubernetes.client import ApiException

from shared import k8sutil

log = logging.getLogger(__name__)


def _scale_statefulset(ns: str, name: str, replicas: int) -> int | None:
    apps = k8sutil.apps()
    try:
        sts = apps.read_namespaced_stateful_set(name, ns)
    except ApiException as e:
        if e.status == 404:
            return None
        raise
    prev = sts.spec.replicas or 0
    if prev == replicas:
        return prev
    apps.patch_namespaced_stateful_set_scale(name, ns, {"spec": {"replicas": replicas}})
    return prev


def _scale_deployment(ns: str, name: str, replicas: int) -> int | None:
    apps = k8sutil.apps()
    try:
        dep = apps.read_namespaced_deployment(name, ns)
    except ApiException as e:
        if e.status == 404:
            return None
        raise
    prev = dep.spec.replicas or 0
    if prev == replicas:
        return prev
    apps.patch_namespaced_deployment_scale(name, ns, {"spec": {"replicas": replicas}})
    return prev


def quiesce(targets: list[dict[str, Any]], *, default_namespace: str, timeout_seconds: int = 900) -> dict[str, Any]:
    """Scale targets to zero. Returns restore state for unquiesce."""
    state: dict[str, Any] = {"targets": []}
    for t in targets or []:
        kind = t.get("kind", "")
        name = t.get("name", "")
        ns = t.get("namespace") or default_namespace
        if not name:
            continue
        prev = None
        if kind == "StatefulSet":
            prev = _scale_statefulset(ns, name, 0)
        elif kind == "Deployment":
            prev = _scale_deployment(ns, name, 0)
        else:
            log.warning("unsupported quiesce kind %s", kind)
            continue
        state["targets"].append({"kind": kind, "name": name, "namespace": ns, "replicas": prev or 0})
    _wait_pods_gone(state["targets"], timeout_seconds)
    return state


def unquiesce(state: dict[str, Any] | None) -> None:
    if not state:
        return
    for t in state.get("targets") or []:
        kind = t.get("kind")
        name = t.get("name")
        ns = t.get("namespace")
        replicas = int(t.get("replicas") or 0)
        if kind == "StatefulSet":
            _scale_statefulset(ns, name, replicas)
        elif kind == "Deployment":
            _scale_deployment(ns, name, replicas)


def _wait_pods_gone(targets: list[dict[str, Any]], timeout: int) -> None:
    core = k8sutil.core()
    deadline = time.time() + timeout
    for t in targets:
        label = None
        # Best-effort: wait until no pods with app label matching name — callers may refine
        while time.time() < deadline:
            pods = core.list_namespaced_pod(t["namespace"], label_selector=f"app={t['name']}")
            active = [p for p in pods.items if p.status.phase not in ("Succeeded", "Failed") and not p.metadata.deletion_timestamp]
            if not active:
                break
            time.sleep(2)
        else:
            log.warning("timeout waiting for pods of %s/%s to stop", t["namespace"], t["name"])
