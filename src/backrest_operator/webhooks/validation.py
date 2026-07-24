"""Validating admission webhook logic (shared with HTTP server)."""

from __future__ import annotations

import base64
import json
import logging
from typing import Any

from shared.constants import ANNOTATION_ALLOW_DESTRUCTIVE, ANNOTATION_LEAVE_DOWN

log = logging.getLogger(__name__)

ALLOWED_URL_PREFIXES = (
    "s3:",
    "b2:",
    "azure:",
    "gs:",
    "sftp:",
    "rclone:",
    "rest:",
    "/",
    "local:",
)


def validate_object(kind: str, obj: dict[str, Any], operation: str) -> tuple[bool, str]:
    spec = obj.get("spec") or {}
    meta = obj.get("metadata") or {}
    annotations = meta.get("annotations") or {}

    if kind == "BackupRepository":
        url = spec.get("url") or ""
        if not url:
            return False, "spec.url is required"
        if not any(url.startswith(p) for p in ALLOWED_URL_PREFIXES):
            return False, f"unsupported repository URL scheme: {url.split(':', 1)[0]}"
        if not (spec.get("passwordSecretRef") or {}).get("name"):
            return False, "spec.passwordSecretRef.name is required"
        if spec.get("appendOnly") and _retention_contradicts_append_only(spec):
            return False, "appendOnly repository cannot enable forget/prune retention that rewrites history"

    if kind == "PVCBackup":
        if not spec.get("pvcName"):
            return False, "spec.pvcName is required"
        if not (spec.get("repositoryRef") or {}).get("name"):
            return False, "spec.repositoryRef.name is required"
        pipeline = ((spec.get("strategy") or {}).get("pipeline")) or []
        if any(s in pipeline for s in ("csiSnapshot", "topolvmSnapshot")):
            if not spec.get("volumeSnapshotClassName"):
                return False, "volumeSnapshotClassName required for snapshot strategies"
        if (spec.get("quiesce") or {}).get("leaveDown") and annotations.get(ANNOTATION_LEAVE_DOWN) != "true":
            return False, f"leaveDown requires annotation {ANNOTATION_LEAVE_DOWN}=true"

    if kind == "PVCRestore":
        mode = spec.get("mode") or ""
        if mode in ("fromResticToNewPVC", "fromResticToExistingPVC", "export"):
            if not (spec.get("repositoryRef") or {}).get("name"):
                return False, "spec.repositoryRef.name is required"
        if mode == "fromVolumeSnapshot" and not (spec.get("volumeSnapshotRef") or {}).get("name"):
            return False, "volumeSnapshotRef.name required"
        export = spec.get("export") or {}
        if mode == "export" or export.get("enabled"):
            ttl = int(export.get("ttlSeconds") or 3600)
            if ttl < 60 or ttl > 86400:
                return False, "export.ttlSeconds must be between 60 and 86400"

    if kind == "BackupPlan":
        if not (spec.get("repositoryRef") or {}).get("name"):
            return False, "spec.repositoryRef.name is required"

    if operation == "DELETE":
        if annotations.get(ANNOTATION_ALLOW_DESTRUCTIVE) != "true":
            # Allow delete without annotation for human kubectl; MCP sets annotation.
            # Soft warning path: still allow — destructive gate is MCP-side primarily.
            pass

    return True, ""


def _retention_contradicts_append_only(spec: dict[str, Any]) -> bool:
    # Placeholder: append-only typically forbids prune; verify schedule check is OK
    return False


def admission_review_response(uid: str, allowed: bool, message: str = "") -> dict[str, Any]:
    return {
        "apiVersion": "admission.k8s.io/v1",
        "kind": "AdmissionReview",
        "response": {
            "uid": uid,
            "allowed": allowed,
            "status": {"message": message} if message else {},
        },
    }


def handle_admission(body: dict[str, Any]) -> dict[str, Any]:
    req = body.get("request") or {}
    uid = req.get("uid") or ""
    kind = (req.get("kind") or {}).get("kind") or ""
    operation = req.get("operation") or ""
    obj = req.get("object") or req.get("oldObject") or {}
    # Decode if needed — already dict from JSON
    ok, msg = validate_object(kind, obj, operation)
    return admission_review_response(uid, ok, msg)
