# Submission Service

Accepts code submissions, hands them to the Judge Worker through RabbitMQ, records the verdicts, and streams each submission's progress to the browser with Server-Sent Events. Written in Go.

The message and HTTP contracts with the other services are in [docs/contracts.md](../../docs/contracts.md).

## How it works

```
POST /submissions ──► Postgres (one transaction) ──► outbox relay ──► jobs.queue ──► Judge Worker
                      submissions + outbox_events                                       │
                                                                                         ▼
browser ◄── SSE ◄── hub ◄── private queue per replica ◄──┐                 verdicts (topic exchange)
                                                          ├────────────────────────────┤
Postgres ◄── guarded UPDATE ◄── shared verdict queue ◄────┘                  catalog.verdicts (verdict.#)
```

| Concern | How it is handled | Code |
|---|---|---|
| A crash or broker outage must not lose a submission | **Transactional outbox.** The request writes the submission and its job message in one Postgres transaction and never talks to RabbitMQ. A background relay publishes pending rows with publisher confirms and marks them `PUBLISHED`. | [repository](internal/repository/postgres.go), [outbox](internal/outbox/relay.go) |
| Delivery is at-least-once, so duplicates happen | **Idempotent, forward-only updates.** `QUEUED → RUNNING → COMPLETED` is enforced in SQL (`WHERE status IN (...)`). A duplicate or late event changes nothing, and the first verdict wins. | [repository](internal/repository/postgres.go) |
| Several replicas | The outbox claims rows with `FOR UPDATE SKIP LOCKED`. One durable queue is shared by all replicas for DB writes. Each replica has its own exclusive queue for live updates, so a browser gets events whichever replica it is connected to. Migrations run under an advisory lock. | [consumer](internal/consumer/consumer.go) |
| No lost or stale live updates | The stream subscribes before reading the current state. After a broker reconnect, every open stream re-reads the database. | [events](internal/handler/events.go), [hub](internal/sse/hub.go) |
| Judge never answers | **Reaper.** Submissions stuck in `QUEUED` (30 min) or `RUNNING` (5 min) are completed as `INTERNAL_ERROR` and announced on the `verdicts` exchange. | [reaper](internal/reaper/reaper.go) |
| Bad messages | Malformed events go to `submission-service.verdict.dlq`. Database errors are retried in place with backoff, so they do not burn the queue's delivery limit. | [consumer](internal/consumer/consumer.go) |

## Run it

From the repository root (Docker Desktop must be running):

```powershell
docker compose --profile fake-worker up -d --build --wait
```

This starts Postgres, RabbitMQ, this service on **http://localhost:8082** and the fake worker. Drop `--profile fake-worker` once the real Judge Worker runs, otherwise the two compete for jobs.

The fake worker picks its verdict from the code:

| Code contains | Verdict |
|---|---|
| `while True` | `TIME_LIMIT_EXCEEDED` |
| `MemoryError` | `MEMORY_LIMIT_EXCEEDED` |
| `raise` | `RUNTIME_ERROR` |
| `wrong` | `WRONG_ANSWER` |
| anything else | `ACCEPTED` |

Locally there is no gateway, so send the user ID yourself:

```bash
U=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa
curl -X POST localhost:8082/api/v1/submissions -H "X-User-Id: $U" \
     -d '{"problem_id":"11111111-1111-1111-1111-111111111111","language":"python","code":"return wrong"}'
curl -N localhost:8082/api/v1/submissions/<submission_id>/events -H "X-User-Id: $U"
```

Other useful places:
- RabbitMQ UI: http://localhost:15672 (`judge` / `judge`)
- Metrics: http://localhost:8082/metrics
- Readiness: http://localhost:8082/readyz

If port 5432 is already taken on your machine, put `POSTGRES_PORT=5433` in a `.env` file at the repo root.

## Configuration

Environment variables. With `APP_PROFILE=local` (the default) everything has a working default. With `APP_PROFILE=openshift`, `DATABASE_URL` and `AMQP_URL` are required, so a missing Secret stops startup instead of connecting to localhost.

| Variable | Default | |
|---|---|---|
| `DATABASE_URL` | `postgres://submission:submission@localhost:5432/submission` | |
| `AMQP_URL` | `amqp://judge:judge@localhost:5672/` | |
| `HTTP_ADDR` | `:8080` | |
| `CORS_ORIGIN` | empty (off) | Only for a frontend served from another origin locally |
| `MAX_CODE_BYTES` | `65536` | |
| `ALLOWED_LANGUAGES` | `python` | Comma-separated |
| `OUTBOX_POLL_INTERVAL` | `250ms` | The submit handler also wakes the relay immediately |
| `OUTBOX_BATCH_SIZE` | `50` | |
| `OUTBOX_MAX_RETRIES` | `10` | Then the event is `FAILED` (see `outbox_failed_events`) |
| `OUTBOX_RETENTION` | `24h` | How long `PUBLISHED` rows are kept |
| `REAPER_INTERVAL` | `30s` | |
| `REAPER_QUEUED_TIMEOUT` | `30m` | Generous, so a contest backlog is not reaped |
| `REAPER_RUNNING_TIMEOUT` | `5m` | |
| `SHUTDOWN_TIMEOUT` | `15s` | |
| `LOG_LEVEL` | `info` | Logs are JSON lines with a `request_id` |

## Endpoints

| | |
|---|---|
| `POST /api/v1/submissions` | Submit (`202` + `Location`) |
| `GET /api/v1/submissions/{id}` | One submission (`?include=code` for the code) |
| `GET /api/v1/submissions?problem_id=&limit=&cursor=` | Your submissions, newest first |
| `GET /api/v1/submissions/{id}/events` | SSE stream |
| `GET /healthz` | Liveness (checks nothing external) |
| `GET /readyz` | Readiness (database and broker) |
| `GET /metrics` | Prometheus |

Useful metrics:
- `outbox_pending_events`, `outbox_lag_seconds`, `outbox_failed_events`
- `verdict_events_total{status,result}`, where `result` is `applied`, `duplicate` or `invalid`
- `sse_active_streams`
- `http_requests_total{route,code}`

## Tests

The integration tests use the real Postgres and RabbitMQ from docker compose, and run in a Linux container. That container gives `-race` (which needs cgo), and it gets around Windows Application Control blocking test binaries.

```powershell
docker compose up -d --wait postgres rabbitmq
.\scripts\test.ps1                                         # vet + all tests with -race
.\scripts\test.ps1 -v -run TestReapStuck ./internal/repository
```

When `TEST_DATABASE_URL` or `TEST_AMQP_URL` is set (as in CI), an unreachable dependency fails the tests instead of skipping them.

## Demos (for the report)

1. **Broker outage.** Run `docker compose stop rabbitmq`, submit a few times (all `202`, `/readyz` is `503`, `outbox_pending_events` rises), then run `docker compose start rabbitmq`. The outbox drains on its own, with no retries used and no submission lost.
2. **Idempotency.** Publish the same verdict twice to the `verdicts` exchange with routing key `status.update` (the RabbitMQ UI → Exchanges → Publish message). The row keeps the first one, and `verdict_events_total{result="duplicate"}` goes up.
3. **Replicas.** Run two instances, submit through one, and stream from the other. The client still gets every event.
4. **Stuck judge.** Set `REAPER_RUNNING_TIMEOUT=5s`, publish only a `RUNNING` event, and the stream ends with `INTERNAL_ERROR` about 5 s later.
