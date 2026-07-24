"""HTTP server for validating admission webhooks."""

from __future__ import annotations

import logging
import os
import ssl
from aiohttp import web

from backrest_operator.webhooks.validation import handle_admission

log = logging.getLogger(__name__)


async def validate(request: web.Request) -> web.Response:
    body = await request.json()
    resp = handle_admission(body)
    return web.json_response(resp)


def create_app() -> web.Application:
    app = web.Application()
    app.router.add_post("/validate", validate)
    app.router.add_post("/validate/{tail:.*}", validate)
    # Match any admission path from ValidatingWebhookConfiguration
    app.router.add_route("POST", "/{path:.*}", validate)
    app.router.add_get("/healthz", lambda r: web.Response(text="ok"))
    return app


async def run_webhook_app() -> None:
    port = int(os.environ.get("WEBHOOK_PORT", "9443"))
    cert = os.environ.get("WEBHOOK_CERT", "/tls/tls.crt")
    key = os.environ.get("WEBHOOK_KEY", "/tls/tls.key")
    app = create_app()
    runner = web.AppRunner(app)
    await runner.setup()
    ssl_ctx = None
    if os.path.exists(cert) and os.path.exists(key):
        ssl_ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ssl_ctx.load_cert_chain(cert, key)
    site = web.TCPSite(runner, "0.0.0.0", port, ssl_context=ssl_ctx)
    await site.start()
    log.info("webhook listening on %s", port)
