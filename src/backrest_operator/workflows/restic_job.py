"""Build and manage restic Jobs for backup/restore/export/check."""

from __future__ import annotations

import logging
from typing import Any

from kubernetes.client import ApiException

from shared import k8sutil
from shared.constants import RESTIC_IMAGE

log = logging.getLogger(__name__)


def _env_from_repo(repo: dict[str, Any]) -> tuple[list[dict[str, Any]], str | None]:
    spec = repo.get("spec") or {}
    env: list[dict[str, Any]] = [
        {"name": "RESTIC_REPOSITORY", "value": spec.get("url") or ""},
    ]
    pref = spec.get("passwordSecretRef") or {}
    if pref.get("name"):
        env.append(
            {
                "name": "RESTIC_PASSWORD",
                "valueFrom": {
                    "secretKeyRef": {
                        "name": pref["name"],
                        "key": pref.get("key") or "RESTIC_PASSWORD",
                    }
                },
            }
        )
    env_secret = spec.get("envFromSecretRef") or {}
    return env, env_secret.get("name")


def build_restic_job(
    *,
    name: str,
    namespace: str,
    repo: dict[str, Any],
    command: list[str],
    pvc_name: str | None = None,
    mount_path: str = "/data",
    node_name: str | None = None,
    ttl_seconds: int = 86400,
    backoff_limit: int = 2,
    labels: dict[str, str] | None = None,
    append_only: bool = False,
) -> dict[str, Any]:
    env, env_from_secret = _env_from_repo(repo)
    if append_only:
        env.append({"name": "RESTIC_APPEND_ONLY", "value": "1"})
    container: dict[str, Any] = {
        "name": "restic",
        "image": RESTIC_IMAGE,
        "command": command,
        "env": env,
        "volumeMounts": [],
    }
    volumes: list[dict[str, Any]] = []
    if pvc_name:
        container["volumeMounts"].append({"name": "data", "mountPath": mount_path})
        volumes.append({"name": "data", "persistentVolumeClaim": {"claimName": pvc_name}})
    if env_from_secret:
        container["envFrom"] = [{"secretRef": {"name": env_from_secret}}]
    pod_spec: dict[str, Any] = {
        "restartPolicy": "Never",
        "enableServiceLinks": False,
        "containers": [container],
        "volumes": volumes,
    }
    if node_name:
        pod_spec["nodeName"] = node_name
    return {
        "apiVersion": "batch/v1",
        "kind": "Job",
        "metadata": {"name": name, "namespace": namespace, "labels": labels or {}},
        "spec": {
            "backoffLimit": backoff_limit,
            "ttlSecondsAfterFinished": ttl_seconds,
            "template": {"metadata": {"labels": labels or {}}, "spec": pod_spec},
        },
    }


def create_or_get_job(body: dict[str, Any]) -> dict[str, Any]:
    batch = k8sutil.batch()
    ns = body["metadata"]["namespace"]
    name = body["metadata"]["name"]
    try:
        return batch.create_namespaced_job(ns, body).to_dict()
    except ApiException as e:
        if e.status == 409:
            return batch.read_namespaced_job(name, ns).to_dict()
        raise


def wait_job(name: str, namespace: str, timeout: int = 3600) -> str:
    import time

    batch = k8sutil.batch()
    deadline = time.time() + timeout
    while time.time() < deadline:
        job = batch.read_namespaced_job(name, namespace)
        status = job.status
        if status.succeeded and status.succeeded > 0:
            return "Succeeded"
        if status.failed and status.failed > (job.spec.backoff_limit or 0):
            return "Failed"
        time.sleep(5)
    return "Timeout"


def build_export_proxy_job(
    *,
    name: str,
    namespace: str,
    repo: dict[str, Any],
    snapshot_id: str,
    path_filters: list[str],
    token: str,
    ttl_seconds: int,
    labels: dict[str, str] | None = None,
) -> dict[str, Any]:
    """Job that restores to a temp dir and serves tar over HTTP with token path."""
    env, env_from_secret = _env_from_repo(repo)
    includes = " ".join(f'"{p}"' for p in path_filters) if path_filters else ""
    script = f"""#!/bin/sh
set -euo pipefail
mkdir -p /work/out
restic restore {snapshot_id} --target /work/out {includes}
cd /work/out && tar -cf /work/archive.tar .
python - <<'PY'
import http.server, socketserver, os, sys
TOKEN = os.environ["EXPORT_TOKEN"]
TTL = int(os.environ.get("EXPORT_TTL", "3600"))
ONESHOT = os.environ.get("EXPORT_ONESHOT", "1") == "1"
PORT = 8080
served = {{"done": False}}

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if served["done"] and ONESHOT:
            self.send_error(410, "gone")
            return
        if self.path.rstrip("/") != f"/{{TOKEN}}/archive.tar":
            self.send_error(403, "forbidden")
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/x-tar")
        self.send_header("Content-Disposition", "attachment; filename=archive.tar")
        self.end_headers()
        with open("/work/archive.tar", "rb") as f:
            self.wfile.write(f.read())
        served["done"] = True
        if ONESHOT:
            sys.stderr.write("oneshot complete\\n")
    def log_message(self, fmt, *args):
        sys.stderr.write("%s\\n" % (fmt % args))

socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("", PORT), Handler) as httpd:
    httpd.socket.settimeout(TTL)
    try:
        while True:
            httpd.handle_request()
            if served["done"] and ONESHOT:
                break
    except Exception as e:
        sys.stderr.write(str(e) + "\\n")
PY
"""
    env = list(env) + [
        {"name": "EXPORT_TOKEN", "value": token},
        {"name": "EXPORT_TTL", "value": str(ttl_seconds)},
        {"name": "EXPORT_ONESHOT", "value": "1"},
    ]
    container: dict[str, Any] = {
        "name": "export",
        "image": RESTIC_IMAGE,
        "command": ["sh", "-c", script],
        "env": env,
        "ports": [{"containerPort": 8080, "name": "http"}],
    }
    if env_from_secret:
        container["envFrom"] = [{"secretRef": {"name": env_from_secret}}]
    # restic image may lack python — use a dual-container friendly image instead
    container["image"] = "ghcr.io/restic/restic:0.19.1"
    # Prefer alpine+restic pattern: use python slim with restic installed via command override
    container["image"] = "python:3.12-alpine"
    container["command"] = [
        "sh",
        "-c",
        "apk add --no-cache restic curl >/dev/null && " + script,
    ]
    return {
        "apiVersion": "batch/v1",
        "kind": "Job",
        "metadata": {"name": name, "namespace": namespace, "labels": labels or {}},
        "spec": {
            "backoffLimit": 1,
            "ttlSecondsAfterFinished": max(ttl_seconds, 300),
            "template": {
                "metadata": {"labels": labels or {}},
                "spec": {
                    "restartPolicy": "Never",
                    "enableServiceLinks": False,
                    "containers": [container],
                },
            },
        },
    }


def ensure_export_service(name: str, namespace: str, job_name: str, labels: dict[str, str]) -> str:
    core = k8sutil.core()
    body = {
        "apiVersion": "v1",
        "kind": "Service",
        "metadata": {"name": name, "namespace": namespace, "labels": labels},
        "spec": {
            "selector": labels,
            "ports": [{"name": "http", "port": 8080, "targetPort": 8080}],
        },
    }
    try:
        core.create_namespaced_service(namespace, body)
    except ApiException as e:
        if e.status != 409:
            raise
    return f"http://{name}.{namespace}.svc:8080"
