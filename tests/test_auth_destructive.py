"""Tests for MCP destructive tool gating and authorization."""

from unittest.mock import patch

import pytest

from backrest_mcp.auth import DESTRUCTIVE_TOOLS, UserIdentity, authorize_tool


@pytest.fixture
def user():
    return UserIdentity(username="alice@example.com", groups=["devs"], uid="uid-1")


def test_destructive_tools_set():
    assert DESTRUCTIVE_TOOLS == {
        "delete_repository",
        "delete_plan",
        "delete_snapshot",
    }


def test_destructive_tool_denied_without_flag(user):
    with patch("backrest_mcp.auth.subject_access_review", return_value=True):
        for tool in DESTRUCTIVE_TOOLS:
            assert authorize_tool(user, tool, "app", allow_destructive=False) is False


def test_destructive_tool_allowed_with_flag_and_sar(user):
    with patch("backrest_mcp.auth.subject_access_review", return_value=True) as sar:
        assert authorize_tool(user, "delete_plan", "app", allow_destructive=True) is True
        sar.assert_called_once()


def test_non_destructive_tool_allowed_without_flag(user):
    with patch("backrest_mcp.auth.subject_access_review", return_value=True):
        assert authorize_tool(user, "list_plans", "app", allow_destructive=False) is True


def test_unknown_tool_denied(user):
    with patch("backrest_mcp.auth.subject_access_review", return_value=True):
        assert authorize_tool(user, "nonexistent_tool", "app", allow_destructive=True) is False


def test_sar_denial_blocks_even_with_destructive_flag(user):
    with patch("backrest_mcp.auth.subject_access_review", return_value=False):
        assert authorize_tool(user, "delete_repository", "app", allow_destructive=True) is False
