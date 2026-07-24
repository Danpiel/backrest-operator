"""Flush helpers (exec into pod or script for Job pre-step)."""

from __future__ import annotations

import logging
from typing import Any

from kubernetes.stream import stream

from shared import k8sutil

log = logging.getLogger(__name__)


def run_flush(flush: dict[str, Any] | None, *, namespace: str) -> None:
    if not flush or not flush.get("enabled"):
        return
    mode = flush.get("mode") or "exec"
    if mode == "script":
        script = flush.get("script") or ""
        if not script.strip():
            log.info("flush script empty; skipping")
        return
    target = flush.get("targetPod") or {}
    selector = target.get("labelSelector") or {}
    if isinstance(selector, dict):
        label_selector = ",".join(f"{k}={v}" for k, v in selector.items())
    else:
        label_selector = str(selector)
    if not label_selector:
        raise ValueError("flush.targetPod.labelSelector is required for exec mode")
    core = k8sutil.core()
    pods = core.list_namespaced_pod(namespace, label_selector=label_selector)
    running = [p for p in pods.items if p.status.phase == "Running"]
    if not running:
        raise RuntimeError(f"no running pods for flush selector {label_selector}")
    pod = running[0]
    container = target.get("container") or pod.spec.containers[0].name
    command = target.get("command") or ["true"]
    log.info("flush exec %s/%s container=%s", namespace, pod.metadata.name, container)
    resp = stream(
        core.connect_get_namespaced_pod_exec,
        pod.metadata.name,
        namespace,
        command=command,
        container=container,
        stderr=True,
        stdin=False,
        stdout=True,
        tty=False,
    )
    log.debug("flush output: %s", resp)
