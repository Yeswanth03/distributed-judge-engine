# AI Feedback Service

Consumes failed verdicts, generates feedback with Ollama, and stores feedback in
its own PostgreSQL database. The service also exposes an API for requesting
feedback on demand and retrieving saved results. Submission and problem details
are currently mocked.

## Run locally

From the repository root, start PostgreSQL and RabbitMQ:

```bash
docker compose up -d postgres rabbitmq
```

Start Ollama separately and make sure the configured model is available (the
default is `llama3.2`). Then run the AI service:

```bash
cd services/ai-service
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/python -m app.main
```

Swagger UI is at <http://localhost:8084/docs>.

## API

The gateway must authenticate the user and pass a trusted `X-User-ID` header.
For local Swagger use, enter a sample user ID directly. In deployment, the AI
service must only be reachable through a gateway that strips client-supplied
`X-User-ID` headers and sets its own.

### Request feedback

```http
POST /submissions/{submission_id}/feedback
X-User-ID: user-42
Idempotency-Key: feedback-attempt-1
```

No body is required. The service creates a request, calls Ollama, saves the
result, and returns it. Because generation is synchronous in this increment,
the request waits for Ollama. Reusing the same idempotency key for the same user
and submission returns the existing request rather than generating another
result.

### Get one feedback request

```http
GET /feedback-requests/{request_id}
X-User-ID: user-42
```

### Get latest feedback for a submission

```http
GET /submissions/{submission_id}/feedback
X-User-ID: user-42
```

Feedback responses include `request_id`, `submission_id`, `problem_id`,
`verdict`, `rejudge_seq`, `status`, `feedback`, model and prompt versions,
token counts, and timestamps.

## Storage and message handling

`feedback_requests` stores the request status and generated feedback. Its unique
`(user_id, idempotency_key)` constraint makes retries of the same request safe.
Failed verdict events use a deterministic key derived from `submission_id` and
`rejudge_seq`; on-demand requests use the caller's `Idempotency-Key`.

The RabbitMQ consumer binds its own durable queue to `verdict.wrong_answer`,
`verdict.time_limit_exceeded`, `verdict.memory_limit_exceeded`,
`verdict.runtime_error`, and `verdict.compile_error`. It acknowledges a valid
event only after the feedback record is saved. Invalid messages go to
`ai.feedback.dlq`.

## Configuration

All settings use the `AI_` environment-variable prefix.

| Variable | Default | Meaning |
|---|---|---|
| `AI_RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | RabbitMQ connection |
| `AI_VERDICTS_EXCHANGE` | `verdicts` | Judge Worker topic exchange |
| `AI_FEEDBACK_QUEUE` | `ai.feedback.jobs` | AI-owned durable queue |
| `AI_DEAD_LETTER_EXCHANGE` | `verdicts.dlx` | Dead-letter exchange |
| `AI_DEAD_LETTER_QUEUE` | `ai.feedback.dlq` | Invalid-message queue |
| `AI_DATABASE_URL` | `postgresql://postgres:postgres@localhost:5432/ai_feedback` | AI database |
| `AI_OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama server |
| `AI_OLLAMA_MODEL` | `llama3.2` | Installed local model name |
| `AI_OLLAMA_TIMEOUT_SECONDS` | `120` | Ollama request timeout |
| `AI_PROMPT_VERSION` | `1` | Prompt version stored with each response |
| `AI_API_HOST` | `0.0.0.0` | HTTP bind address |
| `AI_API_PORT` | `8084` | HTTP port |

The mock provider currently returns the same sample problem and code for any
submission ID. Replace it with authenticated Submission Service and safe Catalog
lookups before using real submissions. Never send hidden test inputs or expected
outputs to Ollama. The API trusts `X-User-ID` only behind the trusted gateway.
