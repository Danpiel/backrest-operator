"""Tests for shared.filters namespace allow-list helpers."""

from shared.filters import filter_by_namespace, namespace_allowed


def test_namespace_allowed_all_when_allow_is_none():
    assert namespace_allowed("any-ns", allow=None) is True
    assert namespace_allowed("", allow=None) is True


def test_namespace_allowed_empty_list_denies_all():
    assert namespace_allowed("app", allow=[]) is False


def test_namespace_allowed_membership():
    allow = ["backrest-system", "app"]
    assert namespace_allowed("app", allow=allow) is True
    assert namespace_allowed("other", allow=allow) is False


def test_filter_by_namespace_keeps_allowed_only():
    items = [
        {"metadata": {"namespace": "app", "name": "a"}},
        {"metadata": {"namespace": "other", "name": "b"}},
        {"metadata": {"namespace": "backrest-system", "name": "c"}},
        {"metadata": {}, "name": "no-ns"},
    ]
    allow = ["app", "backrest-system"]
    result = filter_by_namespace(items, allow=allow)
    names = [i.get("metadata", {}).get("name") for i in result]
    assert names == ["a", "c"]


def test_filter_by_namespace_missing_metadata_namespace():
    items = [{"metadata": {}}]
    assert filter_by_namespace(items, allow=["default"]) == []
