"""Tests for Prometheus metric registration."""

from prometheus_client import Counter, Gauge, Histogram

from shared import metrics


def test_backup_counters_exist():
    assert isinstance(metrics.BACKUP_TOTAL, Counter)
    assert metrics.BACKUP_TOTAL._labelnames == ("namespace", "name", "result")


def test_backup_histogram_exists():
    assert isinstance(metrics.BACKUP_DURATION, Histogram)
    assert metrics.BACKUP_DURATION._labelnames == ("namespace", "name")


def test_gauges_exist():
    assert isinstance(metrics.BACKUP_LAST_SUCCESS, Gauge)
    assert isinstance(metrics.REPO_STORAGE_BYTES, Gauge)
    assert isinstance(metrics.REPO_STORAGE_RATIO, Gauge)
    assert isinstance(metrics.SNAPSHOT_COPIES, Gauge)


def test_error_and_auth_counters_exist():
    assert isinstance(metrics.RECONCILE_ERRORS, Counter)
    assert isinstance(metrics.MCP_AUTH_DENIALS, Counter)
    assert metrics.RECONCILE_ERRORS._labelnames == ("kind",)
    assert metrics.MCP_AUTH_DENIALS._labelnames == ("tool",)
