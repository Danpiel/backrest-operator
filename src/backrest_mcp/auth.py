"""Kubernetes auth helpers for MCP: TokenReview, SAR, impersonation."""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any

from kubernetes import client
from kubernetes.client import ApiException

from shared import k8sutil
from shared.metrics import MCP_AUTH_DENIALS

log = logging.getLogger(__name__)


@dataclass
class UserIdentity:
    username: str
    groups: list[str]
    uid: str = ""


def review_token(token: str) -> UserIdentity | None:
    api = k8sutil.authn()
    body = client.V1TokenReview(spec=client.V1TokenReviewSpec(token=token))
    try:
        result = api.create_token_review(body)
    except ApiException as e:
        log.warning("TokenReview failed: %s", e)
        return None
    status = result.status
    if not status or not status.authenticated:
        return None
    user = status.user
    if not user or not user.username:
        return None
    return UserIdentity(username=user.username, groups=list(user.groups or []), uid=user.uid or "")


def subject_access_review(
    user: UserIdentity,
    *,
    namespace: str,
    verb: str,
    group: str,
    resource: str,
    name: str = "",
) -> bool:
    api = k8sutil.authz()
    attrs = client.V1ResourceAttributes(
        namespace=namespace,
        verb=verb,
        group=group,
        resource=resource,
        name=name or None,
    )
    body = client.V1SubjectAccessReview(
        spec=client.V1SubjectAccessReviewSpec(
            user=user.username,
            groups=user.groups,
            resource_attributes=attrs,
        )
    )
    try:
        result = api.create_subject_access_review(body)
    except ApiException as e:
        log.warning("SAR failed: %s", e)
        return False
    return bool(result.status and result.status.allowed)


def impersonating_headers(user: UserIdentity) -> dict[str, str]:
    headers = {"Impersonate-User": user.username}
    for g in user.groups:
        # kubernetes client uses Impersonate-Group repeatedly — encode as list later
        pass
    return headers


def deny(tool: str) -> None:
    MCP_AUTH_DENIALS.labels(tool).inc()


TOOL_PERMISSIONS: dict[str, tuple[str, str, str]] = {
    # tool -> (verb, resource plural, group)
    "list_clusters": ("list", "backrestclusters", "operator.backrest.io"),
    "get_cluster": ("get", "backrestclusters", "operator.backrest.io"),
    "list_repositories": ("list", "backuprepositories", "operator.backrest.io"),
    "get_repository": ("get", "backuprepositories", "operator.backrest.io"),
    "create_repository": ("create", "backuprepositories", "operator.backrest.io"),
    "update_repository": ("update", "backuprepositories", "operator.backrest.io"),
    "delete_repository": ("delete", "backuprepositories", "operator.backrest.io"),
    "list_plans": ("list", "backupplans", "operator.backrest.io"),
    "get_plan": ("get", "backupplans", "operator.backrest.io"),
    "create_plan": ("create", "backupplans", "operator.backrest.io"),
    "update_plan": ("update", "backupplans", "operator.backrest.io"),
    "delete_plan": ("delete", "backupplans", "operator.backrest.io"),
    "trigger_backup": ("create", "pvcbackups", "operator.backrest.io"),
    "list_snapshots": ("get", "backuprepositories", "operator.backrest.io"),
    "get_snapshot": ("get", "backuprepositories", "operator.backrest.io"),
    "delete_snapshot": ("delete", "backuprepositories", "operator.backrest.io"),
    "create_pvc_backup": ("create", "pvcbackups", "operator.backrest.io"),
    "get_pvc_backup": ("get", "pvcbackups", "operator.backrest.io"),
    "create_pvc_restore": ("create", "pvcrestores", "operator.backrest.io"),
    "get_pvc_restore": ("get", "pvcrestores", "operator.backrest.io"),
    "restore_export": ("create", "pvcrestores", "operator.backrest.io"),
    "repo_status": ("get", "backuprepositories", "operator.backrest.io"),
}

DESTRUCTIVE_TOOLS = {
    "delete_repository",
    "delete_plan",
    "delete_snapshot",
}


def authorize_tool(user: UserIdentity, tool: str, namespace: str, *, allow_destructive: bool = False) -> bool:
    if tool in DESTRUCTIVE_TOOLS and not allow_destructive:
        deny(tool)
        return False
    perm = TOOL_PERMISSIONS.get(tool)
    if not perm:
        deny(tool)
        return False
    verb, resource, group = perm
    ok = subject_access_review(user, namespace=namespace, verb=verb, group=group, resource=resource)
    if not ok:
        deny(tool)
    return ok
