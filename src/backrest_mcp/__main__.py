"""MCP server entrypoint — stdio and HTTP/SSE."""

from __future__ import annotations

import json
import logging
import os
from typing import Any

from aiohttp import web

from backrest_mcp import tools
from backrest_mcp.auth import UserIdentity, authorize_tool, review_token
from shared.metrics import start_metrics_server

log = logging.getLogger(__name__)

# Context var for current user (HTTP)
_current_user: UserIdentity | None = None


TOOL_SCHEMAS: list[dict[str, Any]] = [
    {"name": "list_clusters", "description": "List BackrestCluster resources", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}}, "required": []}},
    {"name": "get_cluster", "description": "Get a BackrestCluster", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "name": {"type": "string"}}, "required": ["namespace", "name"]}},
    {"name": "list_repositories", "description": "List BackupRepositories", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}}, "required": ["namespace"]}},
    {"name": "get_repository", "description": "Get BackupRepository", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "name": {"type": "string"}}, "required": ["namespace", "name"]}},
    {"name": "create_repository", "description": "Create BackupRepository", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "body": {"type": "object"}}, "required": ["namespace", "body"]}},
    {"name": "delete_repository", "description": "Delete BackupRepository (destructive)", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "name": {"type": "string"}, "allow_destructive": {"type": "boolean"}}, "required": ["namespace", "name"]}},
    {"name": "list_plans", "description": "List BackupPlans", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}}, "required": ["namespace"]}},
    {"name": "create_plan", "description": "Create BackupPlan", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "body": {"type": "object"}}, "required": ["namespace", "body"]}},
    {"name": "delete_plan", "description": "Delete BackupPlan (destructive)", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "name": {"type": "string"}, "allow_destructive": {"type": "boolean"}}, "required": ["namespace", "name"]}},
    {"name": "trigger_backup", "description": "Create PVCBackup to trigger a backup", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "body": {"type": "object"}}, "required": ["namespace", "body"]}},
    {"name": "create_pvc_restore", "description": "Create PVCRestore", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "body": {"type": "object"}}, "required": ["namespace", "body"]}},
    {"name": "get_pvc_restore", "description": "Get PVCRestore status (includes exportURL)", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "name": {"type": "string"}}, "required": ["namespace", "name"]}},
    {"name": "restore_export", "description": "Create export restore Job and curl URL", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "repository_name": {"type": "string"}, "repository_namespace": {"type": "string"}, "snapshot_id": {"type": "string"}, "path_filters": {"type": "array", "items": {"type": "string"}}, "ttl_seconds": {"type": "integer"}}, "required": ["namespace", "repository_name"]}},
    {"name": "repo_status", "description": "Repository status summary", "inputSchema": {"type": "object", "properties": {"namespace": {"type": "string"}, "name": {"type": "string"}}, "required": ["namespace", "name"]}},
]


def _resolve_user(request: web.Request | None) -> UserIdentity:
    global _current_user
    if request is not None:
        auth = request.headers.get("Authorization", "")
        if auth.lower().startswith("bearer "):
            token = auth.split(" ", 1)[1].strip()
            user = review_token(token)
            if user:
                return user
        raise web.HTTPUnauthorized(text="Bearer Kubernetes token required")
    # stdio: treat as local kubeconfig user — synthetic identity for local SAR via SA
    # For stdio, use a placeholder that means "use in-cluster/local client permissions"
    return UserIdentity(username="mcp-stdio-local", groups=["system:authenticated"])


def _call_tool(name: str, arguments: dict[str, Any], user: UserIdentity) -> Any:
    ns = arguments.get("namespace") or "default"
    allow = bool(arguments.get("allow_destructive"))
    # stdio local bypasses TokenReview but still checks allow_destructive
    if user.username != "mcp-stdio-local":
        if not authorize_tool(user, name, ns, allow_destructive=allow):
            raise PermissionError(f"forbidden: {name}")
    elif name in ("delete_repository", "delete_plan", "delete_snapshot") and not allow:
        raise PermissionError("allow_destructive=true required")

    if name == "list_clusters":
        return tools.list_clusters(arguments.get("namespace") or "")
    if name == "get_cluster":
        return tools.get_cluster(ns, arguments["name"])
    if name == "list_repositories":
        return tools.list_repositories(ns)
    if name == "get_repository":
        return tools.get_repository(ns, arguments["name"])
    if name == "create_repository":
        return tools.create_repository(ns, arguments["body"])
    if name == "delete_repository":
        return tools.delete_repository(ns, arguments["name"], allow_destructive=allow)
    if name == "list_plans":
        return tools.list_plans(ns)
    if name == "create_plan":
        return tools.create_plan(ns, arguments["body"])
    if name == "delete_plan":
        return tools.delete_plan(ns, arguments["name"], allow_destructive=allow)
    if name == "trigger_backup":
        return tools.trigger_backup(ns, arguments["body"])
    if name == "create_pvc_restore":
        return tools.create_pvc_restore(ns, arguments["body"])
    if name == "get_pvc_restore":
        return tools.get_pvc_restore(ns, arguments["name"])
    if name == "restore_export":
        return tools.restore_export(
            ns,
            repository_name=arguments["repository_name"],
            repository_namespace=arguments.get("repository_namespace"),
            snapshot_id=arguments.get("snapshot_id") or "latest",
            path_filters=arguments.get("path_filters"),
            ttl_seconds=int(arguments.get("ttl_seconds") or 3600),
        )
    if name == "repo_status":
        return tools.repo_status(ns, arguments["name"])
    raise ValueError(f"unknown tool {name}")


async def handle_mcp_http(request: web.Request) -> web.Response:
    """Minimal JSON-RPC style MCP over HTTP for list/call tools."""
    user = _resolve_user(request)
    body = await request.json()
    method = body.get("method")
    req_id = body.get("id")
    if method == "tools/list":
        # Filter tools by SAR
        visible = []
        for t in TOOL_SCHEMAS:
            ns = "default"
            if user.username == "mcp-stdio-local" or authorize_tool(user, t["name"], ns, allow_destructive=True):
                # list with destructive visible but still gated on call
                if authorize_tool(user, t["name"], ns, allow_destructive=t["name"] not in ("delete_repository", "delete_plan", "delete_snapshot")) or t["name"] in ("delete_repository", "delete_plan", "delete_snapshot"):
                    # Show tool if user has base verb; destructive still needs flag on call
                    base_ok = authorize_tool(user, t["name"], ns, allow_destructive=True) if t["name"] in ("delete_repository", "delete_plan", "delete_snapshot") else authorize_tool(user, t["name"], ns, allow_destructive=False)
                    if user.username == "mcp-stdio-local" or base_ok:
                        visible.append(t)
        # Simplify: for authenticated users show all schemas; enforce on call
        visible = TOOL_SCHEMAS
        return web.json_response({"jsonrpc": "2.0", "id": req_id, "result": {"tools": visible}})
    if method == "tools/call":
        params = body.get("params") or {}
        try:
            result = _call_tool(params.get("name"), params.get("arguments") or {}, user)
            return web.json_response(
                {
                    "jsonrpc": "2.0",
                    "id": req_id,
                    "result": {"content": [{"type": "text", "text": json.dumps(result, default=str)}]},
                }
            )
        except PermissionError as e:
            return web.json_response({"jsonrpc": "2.0", "id": req_id, "error": {"code": 403, "message": str(e)}}, status=403)
        except Exception as e:
            log.exception("tool call")
            return web.json_response({"jsonrpc": "2.0", "id": req_id, "error": {"code": 500, "message": str(e)}}, status=500)
    return web.json_response({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32601, "message": "method not found"}}, status=404)


def run_stdio() -> None:
    """Simple stdio JSON-RPC loop."""
    import sys

    user = UserIdentity(username="mcp-stdio-local", groups=["system:authenticated"])
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            body = json.loads(line)
        except json.JSONDecodeError:
            continue
        method = body.get("method")
        req_id = body.get("id")
        if method == "tools/list":
            resp = {"jsonrpc": "2.0", "id": req_id, "result": {"tools": TOOL_SCHEMAS}}
        elif method == "tools/call":
            params = body.get("params") or {}
            try:
                result = _call_tool(params.get("name"), params.get("arguments") or {}, user)
                resp = {
                    "jsonrpc": "2.0",
                    "id": req_id,
                    "result": {"content": [{"type": "text", "text": json.dumps(result, default=str)}]},
                }
            except Exception as e:
                resp = {"jsonrpc": "2.0", "id": req_id, "error": {"code": 500, "message": str(e)}}
        elif method == "initialize":
            resp = {
                "jsonrpc": "2.0",
                "id": req_id,
                "result": {
                    "protocolVersion": "2024-11-05",
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "backrest-mcp", "version": "0.1.0"},
                },
            }
        else:
            resp = {"jsonrpc": "2.0", "id": req_id, "error": {"code": -32601, "message": "method not found"}}
        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()


def main() -> None:
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO"))
    start_metrics_server(int(os.environ.get("METRICS_PORT", "8080")))
    mode = os.environ.get("MCP_MODE", "http").lower()
    if mode == "stdio":
        run_stdio()
        return
    app = web.Application()
    app.router.add_post("/mcp", handle_mcp_http)
    app.router.add_get("/healthz", lambda r: web.Response(text="ok"))
    port = int(os.environ.get("MCP_PORT", "8081"))
    web.run_app(app, host="0.0.0.0", port=port)


if __name__ == "__main__":
    main()
