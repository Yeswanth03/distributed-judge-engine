from contextlib import asynccontextmanager

import docker
from fastapi import FastAPI, HTTPException
from fastapi.responses import PlainTextResponse
from prometheus_client import CONTENT_TYPE_LATEST, Counter, Gauge, generate_latest

from .config import settings
from .docker_runner import run_submission
from .models import ExecuteRequest, ExecuteResult
from .pool import Pool, PoolExhausted

docker_client = docker.from_env()
pool = Pool(docker_client, settings)

EXECUTIONS_TOTAL = Counter("spawner_executions_total", "Submissions executed", ["outcome"])
POOL_SIZE = Gauge("spawner_pool_containers", "Containers currently tracked", ["state"])


@asynccontextmanager
async def lifespan(_app: FastAPI):
    pool.start()
    yield
    pool.stop()


app = FastAPI(title="Spawner", lifespan=lifespan)


@app.post("/execute", response_model=ExecuteResult)
def execute(req: ExecuteRequest):
    try:
        cid, entry = pool.claim()
    except PoolExhausted:
        # Backpressure, not a server error: the Judge Worker should leave the job
        # unacked so the broker retries it, rather than treating this as the
        # submission's fault.
        raise HTTPException(status_code=503, detail="pool_exhausted")

    healthy = True
    try:
        result = run_submission(entry.container, req)
        if result.oom_killed or result.exit_code is None:
            healthy = False
        EXECUTIONS_TOTAL.labels(outcome="ok").inc()
        return result
    except Exception:
        healthy = False
        EXECUTIONS_TOTAL.labels(outcome="error").inc()
        raise HTTPException(status_code=500, detail="execution_failed")
    finally:
        pool.release(cid, healthy)


@app.get("/health")
def health():
    return pool.stats()


@app.get("/metrics")
def metrics():
    stats = pool.stats()
    POOL_SIZE.labels(state="idle").set(stats["idle"])
    POOL_SIZE.labels(state="busy").set(stats["busy"])
    POOL_SIZE.labels(state="total").set(stats["total"])
    return PlainTextResponse(generate_latest(), media_type=CONTENT_TYPE_LATEST)
