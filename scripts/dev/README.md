# Local dev/test runbook

Everything here is for running the Judge Worker + Spawner locally, before the
real Catalog/Submission Service are wired in. Run each step in its own
terminal tab (or background them, shown below).

## 1. Fast unit tests (no Docker needed)

```
cd services/spawner && .venv/bin/python -m pytest tests/test_pool.py -v
cd services/judge-worker && .venv/bin/python -m pytest tests/test_judge.py -v
```

## 2. Real-Docker integration tests (needs Docker running)

```
cd services/spawner && .venv/bin/python -m pytest tests/test_docker_integration.py -v
```
Covers the 4 scenarios manually verified during development: correct run,
genuine timeout, over-the-problem's-memory-limit-but-under-the-container's-cap,
and a genuine kernel OOM-kill. Skips automatically if Docker isn't reachable.

## 3. Full end-to-end run (RabbitMQ + Catalog stand-in + real Spawner + real Judge Worker)

From the repo root, in order:

```
# 1. Docker Desktop must be running first.

# 2. Start RabbitMQ
docker compose up -d rabbitmq
# wait for it to report healthy:
docker inspect --format='{{.State.Health.Status}}' judge-engine-rabbitmq

# 3. Start the Catalog stand-in (not the real Catalog, see note below)
cd services/judge-worker && .venv/bin/python ../../scripts/dev/fake_catalog.py &

# 4. Start the Spawner
cd services/spawner && .venv/bin/uvicorn app.main:app --port 8081 &

# 5. Start the Judge Worker
cd services/judge-worker && .venv/bin/python -m app.main &

# 6. Run the smoke test: publishes a real job, waits for the real verdict
# (from the repo root; there's no venv there, use either service's venv, both have pika)
services/judge-worker/.venv/bin/python scripts/dev/e2e_smoke_test.py
```

Expect: a verdict printed with `"verdict": "accepted"`, and the script exits 0.

Check things are healthy at any point:
```
curl -s http://localhost:8081/health   # spawner pool state
curl -s http://localhost:8082/health   # judge worker
curl -s -u guest:guest http://localhost:15672/api/queues/%2F/judge.jobs   # queue depth, should be 0 ready/unacked after the test
```

Stop everything:
```
pkill -f "uvicorn app.main:app"
pkill -f "app.main"
pkill -f "fake_catalog"
docker compose down
```

## Why a fake Catalog

`scripts/dev/fake_catalog.py` implements the exact same contract as the real
`services/catalog`'s `/internal/problems/{id}/testset` endpoint (same path,
same `X-Service-Token` header, same JSON shape), so the Judge Worker's real
HTTP client code is genuinely exercised. The real Catalog needs Postgres +
Maven running too, which is a separate setup step; swap `catalog_base_url` to
point at it once that's up and everything else here works unchanged.

## What's been fixed because of running this for real

Running the actual Docker daemon surfaced two bugs that a mocked test never
would have:
- `put_archive`/`get_archive` (how files were first written into containers)
  flatly refuse to work on a read-only-rootfs container, even for a writable
  tmpfs path. Fixed by writing/reading through live `exec` instead
  (`app/docker_runner.py`).
- A transient Docker failure (Docker Desktop's idle auto-pause) used to kill
  the Spawner's background pool-management threads permanently, and dropped
  RabbitMQ's connection used to crash the Judge Worker process outright. Both
  now catch, log, and retry instead of dying (`app/pool.py`,
  `app/broker.py`), with a regression test for the first one in
  `test_pool.py`.
