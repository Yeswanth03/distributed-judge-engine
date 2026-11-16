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
   rows, plus `processing` rows that are older than the maximum generation time.
   Work saved before a crash is therefore finished after the restart.

## API

The gateway authenticates the user and forwards a trusted `X-User-Id` header. This
service must only be reachable through it.

| Method | Path | Result |
|---|---|---|
| `POST` | `/submissions/{submission_id}/feedback` | `202` with a `queued`/`processing` request, or `200` if feedback is already `ready`. No body. |
| `GET` | `/feedback-requests/{request_id}` | Status and feedback of one request |
| `GET` | `/submissions/{submission_id}/feedback` | The latest request for the submission |
| `GET` | `/health` | Liveness |

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
| `AI_DATABASE_URL` | `postgresql://postgres:postgres@localhost:5432/ai_feedback` | AI database |
| `AI_OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama server |
| `AI_OLLAMA_MODEL` | `qwen2.5-coder:7b` | Installed local model name |
| `AI_OLLAMA_TIMEOUT_SECONDS` | `120` | Ollama request timeout |
| `AI_PROMPT_VERSION` | `2` | Prompt version stored with each response |
| `AI_GENERATION_WORKERS` | `2` | Concurrent generations |
| `AI_GENERATION_ATTEMPTS` | `3` | Attempts per request on transient failures |
| `AI_SWEEP_INTERVAL_SECONDS` | `30` | How often queued/stuck requests are resumed |
| `AI_API_HOST` / `AI_API_PORT` | `0.0.0.0` / `8084` | HTTP bind address |

## Open points with other teams

- **Submission Service:** the code is read through the public
  `GET /api/v1/submissions/{id}?include=code`, acting as the submission's owner,
  because no service-to-service endpoint exists yet. If
  `GET /internal/submissions/{id}/source` with `X-Service-Token` is added, only
  `SubmissionClient` changes. The service token is already sent.
- **Judge Worker:** `docs/contracts.md` says the verdict's `problem_id` is Catalog's
  UUID, but the worker currently forwards the slug. The AI service accepts either,
  because it resolves the problem through the submission.
