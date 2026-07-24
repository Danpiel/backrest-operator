"""Tests for BackupPlan → Backrest plan fragment serialization."""

from backrest_operator.workflows.repository import plan_to_backrest_fragment


def test_plan_to_backrest_fragment_full():
    plan = {
        "metadata": {"namespace": "app", "name": "daily"},
        "spec": {
            "repositoryRef": {"name": "primary", "namespace": "backrest-system"},
            "paths": ["/var/lib/myapp"],
            "excludes": ["/var/lib/myapp/tmp"],
            "schedule": "0 2 * * *",
            "retention": {"keepLast": 7, "keepDaily": 14},
            "hooks": [{"name": "pre-backup", "command": "echo ok"}],
            "tags": ["daily"],
        },
    }
    fragment = plan_to_backrest_fragment(plan)
    assert fragment == {
        "id": "app-daily",
        "repo": "primary",
        "paths": ["/var/lib/myapp"],
        "excludes": ["/var/lib/myapp/tmp"],
        "schedule": "0 2 * * *",
        "retention": {"keepLast": 7, "keepDaily": 14},
        "hooks": [{"name": "pre-backup", "command": "echo ok"}],
        "tags": ["daily"],
    }


def test_plan_to_backrest_fragment_defaults():
    plan = {
        "metadata": {"namespace": "ns", "name": "manual"},
        "spec": {"repositoryRef": {"name": "repo"}, "paths": ["/"]},
    }
    fragment = plan_to_backrest_fragment(plan)
    assert fragment["id"] == "ns-manual"
    assert fragment["repo"] == "repo"
    assert fragment["paths"] == ["/"]
    assert fragment["excludes"] == []
    assert fragment["schedule"] == ""
    assert fragment["retention"] == {}
    assert fragment["hooks"] == []
    assert fragment["tags"] == []
