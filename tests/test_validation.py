"""Tests for validating admission webhook logic."""

import pytest

from backrest_operator.webhooks.validation import validate_object
from shared.constants import ANNOTATION_LEAVE_DOWN


def _repo(**spec_overrides):
    spec = {
        "url": "s3:https://s3.example.com/bucket",
        "passwordSecretRef": {"name": "backup-repo", "key": "RESTIC_PASSWORD"},
    }
    spec.update(spec_overrides)
    return {"metadata": {"name": "primary"}, "spec": spec}


def _pvc_backup(**spec_overrides):
    spec = {
        "pvcName": "app-data",
        "repositoryRef": {"name": "primary"},
    }
    spec.update(spec_overrides)
    return {"metadata": {"name": "bk"}, "spec": spec}


def _pvc_restore(**spec_overrides):
    spec = {"mode": "export", "repositoryRef": {"name": "primary"}}
    spec.update(spec_overrides)
    return {"metadata": {"name": "restore"}, "spec": spec}


class TestBackupRepository:
    def test_valid_s3_repository(self):
        ok, msg = validate_object("BackupRepository", _repo(), "CREATE")
        assert ok is True
        assert msg == ""

    @pytest.mark.parametrize(
        "url",
        [
            "",
            "ftp://example.com/repo",
        ],
    )
    def test_invalid_repository_url(self, url):
        ok, msg = validate_object("BackupRepository", _repo(url=url), "CREATE")
        assert ok is False
        assert msg

    def test_missing_password_secret(self):
        ok, msg = validate_object(
            "BackupRepository",
            _repo(passwordSecretRef={}),
            "CREATE",
        )
        assert ok is False
        assert "passwordSecretRef" in msg


class TestPVCBackup:
    def test_valid_minimal(self):
        ok, msg = validate_object("PVCBackup", _pvc_backup(), "CREATE")
        assert ok is True

    def test_csi_snapshot_requires_volume_snapshot_class(self):
        obj = _pvc_backup(strategy={"pipeline": ["csiSnapshot"]})
        ok, msg = validate_object("PVCBackup", obj, "CREATE")
        assert ok is False
        assert "volumeSnapshotClassName" in msg

    def test_topolvm_snapshot_requires_volume_snapshot_class(self):
        obj = _pvc_backup(strategy={"pipeline": ["topolvmSnapshot"]})
        ok, msg = validate_object("PVCBackup", obj, "CREATE")
        assert ok is False
        assert "volumeSnapshotClassName" in msg

    def test_csi_snapshot_valid_with_class(self):
        obj = _pvc_backup(
            strategy={"pipeline": ["csiSnapshot"]},
            volumeSnapshotClassName="csi-hostpath-snapclass",
        )
        ok, msg = validate_object("PVCBackup", obj, "CREATE")
        assert ok is True

    def test_leave_down_requires_annotation(self):
        obj = _pvc_backup(quiesce={"leaveDown": True})
        ok, msg = validate_object("PVCBackup", obj, "CREATE")
        assert ok is False
        assert ANNOTATION_LEAVE_DOWN in msg

    def test_leave_down_allowed_with_annotation(self):
        obj = _pvc_backup(quiesce={"leaveDown": True})
        obj["metadata"]["annotations"] = {ANNOTATION_LEAVE_DOWN: "true"}
        ok, msg = validate_object("PVCBackup", obj, "CREATE")
        assert ok is True


class TestPVCRestoreExportTtl:
    @pytest.mark.parametrize("ttl", [59, 86401])
    def test_export_ttl_out_of_bounds(self, ttl):
        obj = _pvc_restore(export={"enabled": True, "ttlSeconds": ttl})
        ok, msg = validate_object("PVCRestore", obj, "CREATE")
        assert ok is False
        assert "ttlSeconds" in msg

    @pytest.mark.parametrize("ttl", [60, 3600, 86400])
    def test_export_ttl_in_bounds(self, ttl):
        obj = _pvc_restore(export={"enabled": True, "ttlSeconds": ttl})
        ok, msg = validate_object("PVCRestore", obj, "CREATE")
        assert ok is True

    def test_export_mode_default_ttl_valid(self):
        ok, msg = validate_object("PVCRestore", _pvc_restore(), "CREATE")
        assert ok is True
