# AI Feedback Service

Generates feedback on code submissions with a local Ollama model and stores it in
its own PostgreSQL database. It gets everything it needs from the other services,
the same way the Judge Worker does:

```
verdicts exchange ──verdict.<failure>──► ai.feedback.jobs ──► AI Service
                                                                  │
   Submission Service  GET /api/v1/submissions/{id}?include=code ◄┤  code, language, problem slug, verdict
   Catalog             GET /problems/{slug}                      ◄┤  problem UUID, statement, limits
   Catalog             GET /internal/problems/{uuid}/testset     ◄┤  test cases (X-Service-Token)
   Ollama              POST /api/generate                        ◄┘
                                                                  ▼
                                                     feedback_requests (PostgreSQL)
                                                                  ▲
   Frontend ── POST/GET /submissions/{id}/feedback, GET /feedback-requests/{id} ──┘
```

**Hidden test cases never reach the model.** Only sample cases are put in the
prompt. When the failing test is hidden, the prompt says only its number.

## Run with Docker Compose (one command)

From the repository root:

```bash
docker compose up -d --wait ai-service
```

This starts PostgreSQL, RabbitMQ, the Submission Service, **Ollama** and the AI service.
Before the AI service starts, a one-off `ollama-pull` container downloads
`qwen2.5-coder:7b`. That's about 4.7 GB on the first run. The model is kept in the
`ollama-models` volume, so later starts only check it's there.

| What | Where |
|---|---|
| AI service (direct, local debugging only) | <http://localhost:8084/docs> |
| AI service through the gateway (`docker compose up -d gateway`) | `http://localhost:8000/api/v1/ai/...`, with `Authorization: Bearer <token>` |
| Ollama in Compose | <http://localhost:11435> (11435, so it never clashes with a native Ollama app) |

The gateway checks the login token, removes any `X-User-Id` the client sent, and
sets the real one. It also strips the `/api/v1/ai` prefix, so
`/api/v1/ai/submissions/{id}/feedback` reaches `/submissions/{id}/feedback` here.

You can override these settings in a `.env` file at the repository root:

| Variable | Default | When to change it |
|---|---|---|
| `AI_OLLAMA_BASE_URL` | `http://ollama:11434` | **On a Mac, set `http://host.docker.internal:11434` to use the native Ollama app.** Docker on a Mac has no GPU access, so the container is much slower. |
| `AI_CATALOG_BASE_URL` | `http://host.docker.internal:8080` | Catalog isn't in Compose yet, so this reaches the Catalog run from the IDE. Set `http://catalog:8080` once it's in Compose. |
| `AI_OLLAMA_MODEL` | `qwen2.5-coder:7b` | To try another model. `ollama-pull` downloads whichever model you set. |
| `AI_SERVICE_PORT` / `OLLAMA_PORT` | `8084` / `11435` | If those host ports are taken |

## Run locally (without Docker for the service)

From the repository root, start PostgreSQL and RabbitMQ:

```bash
docker compose up -d postgres rabbitmq
```

The `ai_feedback` database is created by `deploy/postgres/init`. The service applies
its own migrations (`db/migration`) at startup.

Start Ollama and pull the model (`ollama pull qwen2.5-coder:7b`, about 4.7 GB). It is a
code-specialized model and gave far more accurate feedback than `llama3.2`; any installed
model can be used instead through `AI_OLLAMA_MODEL`. Run the Submission
Service (host port 8082) and Catalog (8080). Then:

```bash
cd services/ai-service
python3.11 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/python -m app.main
```

Swagger UI is at <http://localhost:8084/docs>.

## Tests

```bash
.venv/bin/python -m pytest tests -v
```

These need no broker, database, Ollama or other services. Fake Submission and
Catalog services implement the real paths, headers and JSON shapes, so the real HTTP
clients are exercised.

## Chaos tests

`scripts/chaos.py` breaks the service's dependencies on purpose. It checks that
the service degrades and recovers as designed, using real wrong-answer
submissions:

| Test | What it does | Expected |
|---|---|---|
| `ollama` | Stops the Ollama container while failed verdicts arrive | The Ollama circuit opens, and requests are **deferred** rather than failed. After a restart the circuit closes and every feedback becomes `ready`. |
| `submission` | Stops the Submission Service, then asks for feedback | `502` at first, then **`503 circuit_open`** with `Retry-After`. After a restart, requests work again. |
| `crash` | Kills the AI service in the middle of a generation | After a restart, the orphaned `processing` request is reclaimed by the sweep and completed |

Run it with the whole judging path up: `docker compose up -d --wait ai-service`,
plus Catalog, the Spawner and the Judge Worker. From the repository root:

```bash
python3 services/ai-service/scripts/chaos.py                 # all three tests
python3 services/ai-service/scripts/chaos.py --tests crash   # one test
```

It needs only the Python standard library, and exits non-zero if any test
fails. Keep the Grafana dashboard open while it runs to see the circuits open
and close.

## Flow

1. **Failed verdict.** The consumer binds `ai.feedback.jobs` to
   `verdict.wrong_answer`, `verdict.time_limit_exceeded`,
   `verdict.memory_limit_exceeded`, `verdict.runtime_error` and
   `verdict.compilation_error` (plus the older `verdict.compile_error`). It accepts
   the Judge Worker's UPPERCASE verdict values. It saves a `queued` request keyed
   on `verdict:{submission_id}:{rejudge_seq}`, so a redelivery is a no-op. It acks
   only after the row is saved. Invalid messages go to `ai.feedback.dlq`.
2. **Generation (background).** A worker claims the row (`queued` → `processing`)
   and fetches the code from the Submission Service, passing the submission's owner
   as `X-User-Id`. It then fetches the problem and the judged test set version from
   Catalog, calls Ollama, and saves the result as `ready`.
   - Transient failures (a service down, a 5xx, Ollama errors) are retried
     `AI_GENERATION_ATTEMPTS` times. After that the request is marked `unavailable`
     and `error_reason` says why.
   - A missing submission or problem goes straight to `unavailable`.
3. **Crash safety.** Every `AI_SWEEP_INTERVAL_SECONDS`, a sweep re-submits `queued`
   rows, plus `processing` rows whose heartbeat is older than
   `AI_PROCESSING_STALE_SECONDS`. Each generation attempt refreshes the
   heartbeat, so a request that's still being worked on is never reclaimed. One
   whose worker died, for example because the container was killed, is put back
   in the queue and finished after the restart.

## Resilience

Every call to a dependency goes through a **timeout**, **retries with exponential
backoff and jitter**, and a **circuit breaker** (`app/resilience.py`).

| Dependency | Breaker | Timeout |
|---|---|---|
| Ollama (`POST /api/generate`) | `ollama` | `AI_OLLAMA_TIMEOUT_SECONDS` (120 s) |
| Submission Service | `submission` | `AI_HTTP_TIMEOUT_SECONDS` (5 s) |
| Catalog | `catalog` | `AI_HTTP_TIMEOUT_SECONDS` (5 s) |

Each dependency has its own breaker, so a Catalog outage never blocks calls to
Ollama, and the other way round.

**Circuit breaker states:**
- **CLOSED:** calls go through, and the last 10 outcomes are remembered. Five
  failures among them open the circuit.
- **OPEN:** calls fail immediately with no network call, for 10 s.
- **HALF-OPEN:** after the 10 s, one trial call goes through. If it succeeds, the
  circuit closes and the old failures are forgotten. If it fails, the circuit opens
  again with a fresh timer. Other callers keep failing fast during the trial.

Only signs of an unhealthy dependency count as failures:
- connection errors and timeouts;
- HTTP 5xx and 429;
- for Ollama, an empty or malformed response.

A 404 for someone else's submission is the caller's problem, so it never opens a
circuit.

**Retries:** transient failures are retried up to `AI_GENERATION_ATTEMPTS` times.
The waits double each time (about 1 s, then 2 s, then 4 s), capped at
`AI_RETRY_MAX_SECONDS`, and each wait is randomised between half and the full
value. The randomness stops many requests retrying at the same instant.

**When a circuit is open:**
- **`POST /submissions/{id}/feedback`:** when the Submission Service circuit is
  open, it answers right away with `503` and a `Retry-After` header:

  ```json
  {"status": "circuit_open", "dependency": "submission", "retry_after": 7,
   "message": "submission is temporarily unavailable"}
  ```

- **Background generation:** when the Ollama or Catalog circuit is open, the
  request is **deferred**, not failed. It goes back to `queued`, and `error_reason`
  says what it's waiting for. The sweep retries it once the circuit lets calls
  through again. After `AI_MAX_DEFER_SECONDS` (30 min) it gives up and becomes
  `unavailable`.
- **`GET /health`:** reports each circuit's state. The status is `degraded` while
  any circuit isn't closed. The endpoint still returns `200`, because the process
  itself is alive.

## Metrics and dashboard

`GET /metrics` exposes Prometheus metrics. Prometheus scrapes it every 5 s, and
Grafana shows them on the **AI Feedback Service** dashboard.

```bash
docker compose up -d prometheus grafana
```

| Where | URL |
|---|---|
| Grafana dashboard (opens on it; view without signing in) | <http://localhost:3000> |
| Prometheus (targets, ad-hoc queries) | <http://localhost:9090/targets> |

Prometheus scrapes the AI service both as `ai-service:8084` (in Docker Compose)
and as `host.docker.internal:8084` (running on your machine). Normally only one
of the two is up, and the dashboard adds them together.

| Signal | Metrics |
|---|---|
| Traffic | `ai_http_requests_total{method, route, status}`, `ai_verdict_events_total{verdict}` |
| Errors | 5xx in `ai_http_requests_total`, `ai_feedback_outcomes_total{outcome="unavailable"}`, `ai_dependency_calls_total{outcome="failure"}`, `ai_verdict_events_rejected_total{reason}` |
| Latency | `ai_http_request_duration_seconds`, `ai_llm_generation_duration_seconds`, `ai_feedback_time_to_ready_seconds` (the eventual-consistency window), `ai_dependency_call_duration_seconds` |
| Saturation | `ai_generations_in_flight`, `ai_feedback_requests{status}` (waiting requests) |
| Spend | `ai_llm_tokens_total{model, direction}` |
| Resilience | `ai_circuit_breaker_state{dependency}` (0 closed, 1 half-open, 2 open), `ai_circuit_breaker_transitions_total`, `ai_generation_retries_total` |

Tokens are on the dashboard even though Ollama is free. A retry storm or a
runaway loop shows up there first, and the same panel would track real cost if
the model ever moved to a paid API.

Routes are recorded as templates (`/submissions/{submission_id}/feedback`), and
user or submission IDs are never labels, so the number of time series stays
fixed.

## API

The gateway authenticates the user and forwards a trusted `X-User-Id` header. This
service must only be reachable through it.

| Method | Path | Result |
|---|---|---|
| `POST` | `/submissions/{submission_id}/feedback` | `202` with a `queued`/`processing` request, or `200` if feedback is already `ready`. No body. |
| `POST` | `/submissions/{submission_id}/feedback/direct` | **Accepted submissions only.** Calls the model inside the request: `200` with the hints. Falls back to the background queue (`202`) if the model is too slow or its circuit is open. Anything not accepted gets `409`; use the endpoint above. |
| `GET` | `/feedback-requests/{request_id}` | Status and feedback of one request |
| `GET` | `/submissions/{submission_id}/feedback` | The latest request for the submission |
| `GET` | `/health` | Liveness, plus each dependency's circuit state |
| `GET` | `/metrics` | Prometheus metrics (not in Swagger) |

`POST` works for any final verdict, `ACCEPTED` included. It reuses an existing
`queued`, `processing` or `ready` request for the submission, including the
automatic one for a failed verdict. It starts a new attempt only after
`unavailable`.

POST errors:

| Code | Meaning |
|---|---|
| `404` | The submission does not exist, or is not the caller's |
| `409` | The submission is not judged yet, or is `INTERNAL_ERROR` |
| `502` | The Submission Service is unreachable |

`status` is one of `queued`, `processing`, `ready` or `unavailable`. Responses also
include:
- `request_id`, `submission_id`, `problem_id` (Catalog UUID once resolved)
- `verdict`, `rejudge_seq`, `request_source`
- `feedback`, `error_reason`
- model and prompt versions, token counts, timestamps

## Configuration

All settings use the `AI_` environment-variable prefix.

| Variable | Default | Meaning |
|---|---|---|
| `AI_RABBITMQ_URL` | `amqp://judge:judge@localhost:5672/` | RabbitMQ (login from `deploy/rabbitmq/definitions.json`) |
| `AI_VERDICTS_EXCHANGE` | `verdicts` | Judge Worker topic exchange |
| `AI_FEEDBACK_QUEUE` | `ai.feedback.jobs` | AI-owned durable queue |
| `AI_DEAD_LETTER_EXCHANGE` | `verdicts.dlx` | Dead-letter exchange |
| `AI_DEAD_LETTER_QUEUE` | `ai.feedback.dlq` | Invalid-message queue |
| `AI_SUBMISSION_BASE_URL` | `http://localhost:8082` | Submission Service |
| `AI_CATALOG_BASE_URL` | `http://localhost:8080` | Catalog |
| `AI_INTERNAL_SERVICE_TOKEN` | `local-dev-token` | Must match Catalog's `catalog.internal-token` |
| `AI_HTTP_TIMEOUT_SECONDS` | `5` | Timeout for Submission/Catalog calls |
| `AI_PROCESSING_STALE_SECONDS` | `180` | A `processing` request with no heartbeat for this long is put back in the queue |
| `AI_DATABASE_URL` | `postgresql://postgres:postgres@localhost:5432/ai_feedback` | AI database |
| `AI_OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama server |
| `AI_OLLAMA_MODEL` | `qwen2.5-coder:7b` | Installed local model name |
| `AI_OLLAMA_TIMEOUT_SECONDS` | `120` | Ollama request timeout |
| `AI_OLLAMA_NUM_PREDICT` | `500` | Hard cap on output tokens. The model replies with a JSON object (about 250-300 tokens); a reply cut off by the cap is invalid and retried. |
| `AI_OLLAMA_TEMPERATURE` | `0.2` | Low, so hints are focused and repeatable |
| `AI_OLLAMA_NUM_CTX` | `4096` | Context window (prompts are about 700 tokens) |
| `AI_OLLAMA_KEEP_ALIVE` | `30m` | How long Ollama keeps the model loaded after the last request, so the next hint skips a 4.5 GB reload |
| `AI_DIRECT_TIMEOUT_SECONDS` | `12` | Direct hints (accepted submissions): longest wait for the model inside the request, under the gateway's 15 s. Slower than this, the request continues in the background (`202`). |
| `AI_PROMPT_VERSION` | `8` | Prompt version stored with each response |
| `AI_GENERATION_WORKERS` | `2` | Concurrent generations |
| `AI_GENERATION_ATTEMPTS` | `3` | Attempts per request on transient failures |
| `AI_SWEEP_INTERVAL_SECONDS` | `30` | How often queued/stuck requests are resumed |
| `AI_RETRY_BASE_SECONDS` / `AI_RETRY_MAX_SECONDS` | `1` / `8` | Exponential backoff: first wait and cap |
| `AI_BREAKER_FAILURE_THRESHOLD` | `5` | Failures that open a circuit… |
| `AI_BREAKER_WINDOW_SIZE` | `10` | …among this many most recent calls |
| `AI_BREAKER_OPEN_SECONDS` | `10` | How long a circuit stays open before a trial call |
| `AI_MAX_DEFER_SECONDS` | `1800` | A request waiting on an open circuit gives up after this |
| `AI_API_HOST` / `AI_API_PORT` | `0.0.0.0` / `8084` | HTTP bind address |
| `AI_CORS_ORIGINS` | `*` | Origins allowed to call the service from a browser: `*` or a comma-separated list |

## Open points with other teams

- **Submission Service:** the code is read through the public
  `GET /api/v1/submissions/{id}?include=code`, acting as the submission's owner,
  because no service-to-service endpoint exists yet. If
  `GET /internal/submissions/{id}/source` with `X-Service-Token` is added, only
  `SubmissionClient` changes. The service token is already sent.
- **Judge Worker:** `docs/contracts.md` says the verdict's `problem_id` is Catalog's
  UUID, but the worker currently forwards the slug. The AI service accepts either,
  because it resolves the problem through the submission.
