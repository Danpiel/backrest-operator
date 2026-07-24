"""Watch-namespace / label filters for the cluster-scoped operator."""

from __future__ import annotations

import os
from typing import Iterable


def watched_namespaces() -> list[str] | None:
    """Return allow-list of namespaces, or None for all namespaces."""
    raw = os.environ.get("WATCH_NAMESPACES", "").strip()
    if not raw:
        return None
    return [n.strip() for n in raw.split(",") if n.strip()]


def label_selector() -> str:
    return os.environ.get("WATCH_LABEL_SELECTOR", "").strip()


def namespace_allowed(namespace: str, allow: list[str] | None = None) -> bool:
    allow = watched_namespaces() if allow is None else allow
    if allow is None:
        return True
    return namespace in allow


def filter_by_namespace(items: Iterable[dict], allow: list[str] | None = None) -> list[dict]:
    return [i for i in items if namespace_allowed(i.get("metadata", {}).get("namespace", ""), allow)]
