# Runbook

How to observe the system, break it on purpose and watch it recover. Every command runs from the repository
root with the stack started (`docker compose up -d --build`).

## Observe

**Health.** `docker compose ps` shows every container and its health check. Per service:

| Service | Health endpoint |
| --- | --- |
| Catalog | http://localhost:8080/actuator/health |
| Spawner | http://localhost:8081/health (also shows the sandbox pool: total, idle, busy) |
| Submission | http://localhost:8082/healthz, `/readyz` checks the database and broker |
| AI service | http://localhost:8084/health (includes each dependency's circuit breaker state) |
| User service | http://localhost:8085/healthz |

**Dashboards.** Grafana at http://localhost:3000 opens on *System overview*. There is one dashboard per service,
plus RabbitMQ, Postgres and the gateway.

**Signals to watch.**

| Signal | Where | Normal | Investigate when |
| --- | --- | --- | --- |
| Jobs waiting | RabbitMQ, `jobs.queue` | 0 to a few | Keeps growing: workers are down or too few |
| Dead letters | RabbitMQ, every `*.dlq` queue | 0 | Anything above 0: a message failed its retries |
| Outbox backlog | `outbox_pending_events`, `outbox_lag_seconds` | 0 | Above 0 for long: RabbitMQ is unreachable |
| Sandbox pool | `spawner_pool_containers{state}` | 1 idle when quiet | `busy` stuck at the maximum (10) |
| Leaderboard freshness | `catalog_leaderboard_lag_seconds`, or `lag_seconds` on `GET /leaderboard` | Under 1 s | Above a few seconds: Catalog is behind or down |
| AI availability | `ai_circuit_breaker_state{dependency}` | 0 (closed) | 1 (open): that dependency keeps failing |

**Logs.** `docker compose logs -f <service>`, for example `docker compose logs -f judge-worker spawner`.

## Break it and watch it recover

Submit something first so there is work in flight: use the web app, or the command-line example in the README.

| Failure | How to cause it | What you will see | How it recovers |
| --- | --- | --- | --- |
| Judge workers crash mid-job | `docker kill $(docker compose ps -q judge-worker)` | The job stays unacknowledged in `jobs.queue` and the submission stays `RUNNING` | `docker compose up -d judge-worker`. RabbitMQ redelivers the job and the verdict arrives. Measured: `ACCEPTED` 7 s after submitting, with both workers killed for 5 s |
| Broker outage | `docker compose stop rabbitmq` | Submissions are still accepted (202) because they are written to the outbox. `outbox_pending_events` rises | `docker compose start rabbitmq`. The outbox relay publishes the backlog. Measured: verdict 25 s after submitting, including a 10 s outage |
| AI model unavailable | `docker compose stop ollama` | Verdicts are unaffected. Hints come back `unavailable` with a "try again in a few minutes" message after 2 retries. After 5 failures in a row the Ollama circuit breaker opens | `docker compose start ollama`. New hints work again once the breaker closes |
| Catalog outage | `docker compose stop catalog` | Workers cannot fetch test sets, so each new job is retried 3 times, then moved to `jobs.dlq` (within 15 s). The leaderboard stops updating, and verdicts wait in `catalog.verdicts` | `docker compose start catalog`. Queued verdicts are applied and the leaderboard catches up within seconds. Jobs already in `jobs.dlq` are not retried: after 5 minutes the reaper marks their submissions `INTERNAL_ERROR` with "Please submit again" |
| Load spike | Submit many at once, for example by running the README command-line example in a loop | `jobs.queue` grows and the sandbox pool grows towards its maximum | The queue drains on its own. Add capacity with `docker compose up -d --scale judge-worker=5` |

To inspect or replay a dead-lettered message, open RabbitMQ management (http://localhost:15672), select the
`*.dlq` queue and use *Get messages*.

## Reset

`docker compose down -v` removes all containers and data, and the next `docker compose up -d --build` starts
from clean sample data. It also deletes the downloaded AI model (about 4.7 GB), which is fetched again on the
next start.

## Known limitations

- Judge workers retry a failed job immediately rather than with a delay, so a dependency that is down for
  more than a few seconds (Catalog, the Spawner) sends jobs to the dead-letter queue.
- Time limits are measured as wall-clock time, so heavy load can push a borderline solution over its limit.
- The Spawner holds the Docker socket, which is root-equivalent on the host. Only the Spawner mounts it,
  and the sandboxes have no network, a read-only filesystem, no capabilities and CPU, memory and process limits.
