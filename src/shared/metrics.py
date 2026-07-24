"""Prometheus metrics for operator and MCP."""

from __future__ import annotations

from prometheus_client import Counter, Gauge, Histogram, start_http_server

BACKUP_TOTAL = Counter(
    "backrest_operator_backup_total",
    "PVC backup attempts",
    ["namespace", "name", "result"],
)
BACKUP_DURATION = Histogram(
    "backrest_operator_backup_duration_seconds",
    "PVC backup duration",
    ["namespace", "name"],
    buckets=(30, 60, 120, 300, 600, 1800, 3600, 7200),
)
BACKUP_LAST_SUCCESS = Gauge(
    "backrest_operator_backup_last_success_timestamp",
    "Unix timestamp of last successful backup",
    ["namespace", "name"],
)
REPO_STORAGE_BYTES = Gauge(
    "backrest_operator_repo_storage_bytes",
    "Estimated repository storage usage",
    ["namespace", "name"],
)
REPO_STORAGE_RATIO = Gauge(
    "backrest_operator_repo_storage_ratio",
    "Repository fill ratio 0-1 when capacity known",
    ["namespace", "name"],
)
SNAPSHOT_COPIES = Gauge(
    "backrest_operator_snapshot_copies",
    "Retained snapshot/copy count",
    ["namespace", "name"],
)
RECONCILE_ERRORS = Counter(
    "backrest_operator_reconcile_errors_total",
    "Reconcile errors",
    ["kind"],
)
MCP_AUTH_DENIALS = Counter(
    "backrest_mcp_auth_denials_total",
    "MCP authorization denials",
    ["tool"],
)


def start_metrics_server(port: int = 8080) -> None:
    start_http_server(port)
