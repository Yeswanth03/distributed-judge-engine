# Judge Worker

Pulls submission jobs off RabbitMQ, fetches the problem's hidden test cases from
Catalog, asks the Spawner to run the submission against each one, and publishes a
verdict. Holds no Docker/container-runtime access at all, that lives entirely in
the Spawner service.

Full design rationale and the complete security checklist live in the project's
design doc (ask Sharon for the link).

## Running locally

Needs RabbitMQ, the Catalog service, and the Spawner service all reachable (see
the env vars below for where it looks for each).

```
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/python -m app.main
```

## Tests

```
.venv/bin/python -m pytest tests/ -v
```
These test the verdict-classification logic (`judge.py`) against fake
Catalog/Spawner clients, no broker, no Docker, no network needed.

## Config (env vars, prefix `JUDGE_`)

| Var | Default | Meaning |
|---|---|---|
| `JUDGE_RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | broker connection |
| `JUDGE_CATALOG_BASE_URL` | `http://localhost:8080` | Catalog service |
| `JUDGE_SPAWNER_BASE_URL` | `http://localhost:8081` | Spawner service |
| `JUDGE_INTERNAL_SERVICE_TOKEN` | `local-dev-token` | must match Catalog's `catalog.internal-token` |

## Why there's no time/memory limit on the Job message

Those are fixed per-problem on Catalog (`time_limit_ms`, `memory_limit_mb`) and
fetched from `GET /internal/problems/{id}/testset` alongside the test cases, not
carried on the job itself. See `app/catalog_client.py`.

## Crash safety

The broker job is only acked **after** the verdict is successfully published
(`app/broker.py`). If the worker dies in between, the job gets redelivered and
judged again rather than silently lost; whatever consumes verdicts downstream
(Catalog + Scores) needs to treat them as an idempotent upsert keyed by
`submission_id` so a redelivered duplicate doesn't double-count.
