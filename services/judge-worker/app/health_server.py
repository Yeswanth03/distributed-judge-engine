import threading

import uvicorn
from fastapi import FastAPI
from fastapi.responses import PlainTextResponse
from prometheus_client import CONTENT_TYPE_LATEST, Counter, generate_latest

from .config import settings

JOBS_PROCESSED = Counter("judge_worker_jobs_total", "Jobs judged", ["status"])

app = FastAPI(title="Judge Worker health")


@app.get("/health")
def health():
    return {"status": "ok", "worker_id": settings.worker_id}


@app.get("/metrics")
def metrics():
    return PlainTextResponse(generate_latest(), media_type=CONTENT_TYPE_LATEST)


def start_in_background():
    """Runs the health/metrics endpoint on its own thread. The main thread is busy
    blocking on consume_forever(), but Docker/k8s health checks and Prometheus
    still need somewhere to hit."""
    config = uvicorn.Config(app, host="0.0.0.0", port=settings.health_port, log_level="warning")
    server = uvicorn.Server(config)
    thread = threading.Thread(target=server.run, daemon=True)
    thread.start()
