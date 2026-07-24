"""Kubernetes client helpers."""

from __future__ import annotations

from typing import Any

from kubernetes import client, config


_loaded = False


def load_kube() -> None:
    global _loaded
    if _loaded:
        return
    try:
        config.load_incluster_config()
    except config.ConfigException:
        config.load_kube_config()
    _loaded = True


def core() -> client.CoreV1Api:
    load_kube()
    return client.CoreV1Api()


def apps() -> client.AppsV1Api:
    load_kube()
    return client.AppsV1Api()


def batch() -> client.BatchV1Api:
    load_kube()
    return client.BatchV1Api()


def custom() -> client.CustomObjectsApi:
    load_kube()
    return client.CustomObjectsApi()


def networking() -> client.NetworkingV1Api:
    load_kube()
    return client.NetworkingV1Api()


def authz() -> client.AuthorizationV1Api:
    load_kube()
    return client.AuthorizationV1Api()


def authn() -> client.AuthenticationV1Api:
    load_kube()
    return client.AuthenticationV1Api()


def snapshot() -> client.CustomObjectsApi:
    """VolumeSnapshot lives in snapshot.storage.k8s.io — use custom objects."""
    return custom()


def labels(instance: str, component: str, **extra: str) -> dict[str, str]:
    from shared.constants import LABEL_COMPONENT, LABEL_INSTANCE, LABEL_MANAGED_BY, LABEL_PART_OF, MANAGED_BY, PART_OF

    out = {
        LABEL_MANAGED_BY: MANAGED_BY,
        LABEL_PART_OF: PART_OF,
        LABEL_INSTANCE: instance,
        LABEL_COMPONENT: component,
    }
    out.update({k: v for k, v in extra.items() if v})
    return out


def merge_patch_status(group: str, version: str, plural: str, name: str, namespace: str, status: dict[str, Any]) -> None:
    custom().patch_namespaced_custom_object_status(
        group=group,
        version=version,
        namespace=namespace,
        plural=plural,
        name=name,
        body={"status": status},
    )


def ensure_owner_ref(obj: dict[str, Any], owner: dict[str, Any]) -> dict[str, Any]:
    meta = obj.setdefault("metadata", {})
    refs = meta.setdefault("ownerReferences", [])
    om = owner.get("metadata", {})
    uid = om.get("uid")
    if not uid:
        return obj
    for r in refs:
        if r.get("uid") == uid:
            return obj
    refs.append(
        {
            "apiVersion": owner.get("apiVersion"),
            "kind": owner.get("kind"),
            "name": om.get("name"),
            "uid": uid,
            "controller": True,
            "blockOwnerDeletion": True,
        }
    )
    return obj
