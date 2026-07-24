"""Operator entrypoint."""

from __future__ import annotations

import asyncio
import logging
import os

import kopf

from shared.metrics import start_metrics_server


def main() -> None:
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO"))
    metrics_port = int(os.environ.get("METRICS_PORT", "8080"))
    start_metrics_server(metrics_port)

    # Import handlers for registration
    import backrest_operator.handlers  # noqa: F401

    webhook = os.environ.get("WEBHOOK_ENABLED", "true").lower() in ("1", "true", "yes")

    @kopf.on.startup()
    async def on_startup(**_):
        if webhook:
            from backrest_operator.webhooks.server import run_webhook_app

            asyncio.create_task(run_webhook_app())

    kopf.run(
        clusterwide=True,
        liveness_endpoint="http://0.0.0.0:8081/healthz",
    )


if __name__ == "__main__":
    main()
