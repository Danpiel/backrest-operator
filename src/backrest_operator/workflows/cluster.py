"""Reconcile BackrestCluster → host Deployment + agent DaemonSet/Deployment."""

from __future__ import annotations

import logging
from typing import Any

from kubernetes.client import ApiException

from shared import k8sutil
from shared.constants import (
    BACKREST_PORT,
    DEFAULT_BACKREST_IMAGE,
    DEFAULT_BACKREST_TAG,
    LABEL_CLUSTER,
    LABEL_ROLE,
)

log = logging.getLogger(__name__)


def _image(spec: dict[str, Any]) -> str:
    image = spec.get("image") or DEFAULT_BACKREST_IMAGE
    version = spec.get("version") or DEFAULT_BACKREST_TAG
    if ":" in image.split("/")[-1]:
        return image
    return f"{image}:{version.lstrip('v') if False else version}"


def _host_names(cluster_name: str) -> tuple[str, str, str]:
    return f"backrest-host-{cluster_name}", f"backrest-host-{cluster_name}", f"backrest-host-data-{cluster_name}"


def ensure_cluster(cluster: dict[str, Any]) -> dict[str, Any]:
    meta = cluster.get("metadata") or {}
    name = meta["name"]
    ns = meta["namespace"]
    spec = cluster.get("spec") or {}
    host = spec.get("host") or {}
    agents = spec.get("agents") or {}

    host_name, svc_name, pvc_name = _host_names(name)
    lbl = k8sutil.labels(name, "host", **{LABEL_CLUSTER: name, LABEL_ROLE: "host"})
    image = _image(spec)

    # PVC for host data
    persistence = host.get("persistence") or {}
    size = persistence.get("size") or "20Gi"
    pvc_body = {
        "apiVersion": "v1",
        "kind": "PersistentVolumeClaim",
        "metadata": {"name": pvc_name, "namespace": ns, "labels": lbl},
        "spec": {
            "accessModes": ["ReadWriteOnce"],
            "resources": {"requests": {"storage": size}},
        },
    }
    sc = persistence.get("storageClassName")
    if sc:
        pvc_body["spec"]["storageClassName"] = sc
    core = k8sutil.core()
    try:
        core.create_namespaced_persistent_volume_claim(ns, pvc_body)
    except ApiException as e:
        if e.status != 409:
            raise

    server_url = ((agents.get("multihost") or {}).get("serverURL") or f"http://{svc_name}.{ns}.svc:{BACKREST_PORT}")
    enable_service_links = host.get("enableServiceLinks", False)

    container = {
        "name": "backrest",
        "image": image,
        "ports": [{"containerPort": BACKREST_PORT, "name": "http"}],
        "env": [
            {"name": "BACKREST_PORT", "value": f":{BACKREST_PORT}"},
            {"name": "BACKREST_DATA", "value": "/data"},
            {"name": "BACKREST_MULTIHOST_SERVER_URL", "value": server_url},
        ],
        "volumeMounts": [{"name": "data", "mountPath": "/data"}],
        "readinessProbe": {
            "tcpSocket": {"port": BACKREST_PORT},
            "initialDelaySeconds": 5,
            "periodSeconds": 10,
        },
    }
    if host.get("resources"):
        container["resources"] = host["resources"]

    pod_spec: dict[str, Any] = {
        "enableServiceLinks": bool(enable_service_links),
        "containers": [container],
        "volumes": [{"name": "data", "persistentVolumeClaim": {"claimName": pvc_name}}],
    }
    if host.get("nodeSelector"):
        pod_spec["nodeSelector"] = host["nodeSelector"]
    if host.get("tolerations"):
        pod_spec["tolerations"] = host["tolerations"]

    dep = {
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {"name": host_name, "namespace": ns, "labels": lbl},
        "spec": {
            "replicas": int(host.get("replicas") or 1),
            "selector": {"matchLabels": lbl},
            "template": {"metadata": {"labels": lbl}, "spec": pod_spec},
        },
    }
    apps = k8sutil.apps()
    try:
        apps.create_namespaced_deployment(ns, dep)
    except ApiException as e:
        if e.status == 409:
            apps.patch_namespaced_deployment(host_name, ns, dep)
        else:
            raise

    svc = {
        "apiVersion": "v1",
        "kind": "Service",
        "metadata": {"name": svc_name, "namespace": ns, "labels": lbl},
        "spec": {
            "type": host.get("serviceType") or "ClusterIP",
            "selector": lbl,
            "ports": [{"name": "http", "port": BACKREST_PORT, "targetPort": BACKREST_PORT}],
        },
    }
    try:
        core.create_namespaced_service(ns, svc)
    except ApiException as e:
        if e.status != 409:
            raise

    _ensure_ingress(cluster, svc_name)
    agents_status = _ensure_agents(cluster, server_url, image)

    host_ready = False
    try:
        d = apps.read_namespaced_deployment(host_name, ns)
        host_ready = (d.status.ready_replicas or 0) >= 1
    except ApiException:
        pass

    phase = "Ready" if host_ready and agents_status["agentsReady"] >= agents_status["agentsDesired"] else "Pending"
    if host_ready and agents_status["agentsDesired"] and agents_status["agentsReady"] < agents_status["agentsDesired"]:
        phase = "Degraded"

    return {
        "phase": phase,
        "hostReady": host_ready,
        "agentsReady": agents_status["agentsReady"],
        "agentsDesired": agents_status["agentsDesired"],
        "multihostPaired": agents_status["agentsReady"] if host_ready else 0,
        "conditions": [],
    }


def _ensure_ingress(cluster: dict[str, Any], svc_name: str) -> None:
    meta = cluster["metadata"]
    ns, name = meta["namespace"], meta["name"]
    ingress_spec = ((cluster.get("spec") or {}).get("host") or {}).get("ingress") or {}
    if not ingress_spec.get("enabled"):
        return
    net = k8sutil.networking()
    ing_name = f"backrest-host-{name}"
    rules = [
        {
            "host": ingress_spec.get("host") or "backrest.example.com",
            "http": {
                "paths": [
                    {
                        "path": "/",
                        "pathType": "Prefix",
                        "backend": {"service": {"name": svc_name, "port": {"number": BACKREST_PORT}}},
                    }
                ]
            },
        }
    ]
    body: dict[str, Any] = {
        "apiVersion": "networking.k8s.io/v1",
        "kind": "Ingress",
        "metadata": {"name": ing_name, "namespace": ns},
        "spec": {"rules": rules},
    }
    if ingress_spec.get("className"):
        body["spec"]["ingressClassName"] = ingress_spec["className"]
    if ingress_spec.get("tls"):
        body["spec"]["tls"] = ingress_spec["tls"]
    try:
        net.create_namespaced_ingress(ns, body)
    except ApiException as e:
        if e.status == 409:
            net.patch_namespaced_ingress(ing_name, ns, body)
        else:
            raise


def _ensure_agents(cluster: dict[str, Any], server_url: str, image: str) -> dict[str, int]:
    meta = cluster["metadata"]
    ns, name = meta["namespace"], meta["name"]
    agents = (cluster.get("spec") or {}).get("agents") or {}
    if not agents.get("enabled", True):
        return {"agentsReady": 0, "agentsDesired": 0}

    lbl = k8sutil.labels(name, "agent", **{LABEL_CLUSTER: name, LABEL_ROLE: "agent"})
    container = {
        "name": "backrest",
        "image": image,
        "env": [
            {"name": "BACKREST_PORT", "value": f":{BACKREST_PORT}"},
            {"name": "BACKREST_DATA", "value": "/data"},
            {"name": "BACKREST_MULTIHOST_SERVER_URL", "value": server_url},
        ],
        "volumeMounts": [{"name": "data", "mountPath": "/data"}],
    }
    if agents.get("resources"):
        container["resources"] = agents["resources"]
    pod_spec: dict[str, Any] = {
        "enableServiceLinks": False,
        "containers": [container],
        "volumes": [{"name": "data", "emptyDir": {}}],
    }
    if agents.get("nodeSelector"):
        pod_spec["nodeSelector"] = agents["nodeSelector"]
    if agents.get("tolerations"):
        pod_spec["tolerations"] = agents["tolerations"]

    apps = k8sutil.apps()
    mode = agents.get("mode") or "DaemonSet"
    ds_name = f"backrest-agent-{name}"
    if mode == "Deployment":
        body = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {"name": ds_name, "namespace": ns, "labels": lbl},
            "spec": {
                "replicas": int(agents.get("replicas") or 1),
                "selector": {"matchLabels": lbl},
                "template": {"metadata": {"labels": lbl}, "spec": pod_spec},
            },
        }
        try:
            apps.create_namespaced_deployment(ns, body)
        except ApiException as e:
            if e.status == 409:
                apps.patch_namespaced_deployment(ds_name, ns, body)
            else:
                raise
        try:
            d = apps.read_namespaced_deployment(ds_name, ns)
            return {"agentsReady": d.status.ready_replicas or 0, "agentsDesired": d.spec.replicas or 0}
        except ApiException:
            return {"agentsReady": 0, "agentsDesired": int(agents.get("replicas") or 1)}

    body = {
        "apiVersion": "apps/v1",
        "kind": "DaemonSet",
        "metadata": {"name": ds_name, "namespace": ns, "labels": lbl},
        "spec": {
            "selector": {"matchLabels": lbl},
            "template": {"metadata": {"labels": lbl}, "spec": pod_spec},
        },
    }
    try:
        apps.create_namespaced_daemon_set(ns, body)
    except ApiException as e:
        if e.status == 409:
            apps.patch_namespaced_daemon_set(ds_name, ns, body)
        else:
            raise
    try:
        ds = apps.read_namespaced_daemon_set(ds_name, ns)
        return {
            "agentsReady": ds.status.number_ready or 0,
            "agentsDesired": ds.status.desired_number_scheduled or 0,
        }
    except ApiException:
        return {"agentsReady": 0, "agentsDesired": 0}
