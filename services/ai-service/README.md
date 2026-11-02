# AI Service: mock feedback generation

The service consumes failed verdict events from the Judge Worker's RabbitMQ
topic exchange, looks up mock submission/problem data by the event's IDs, and
asks Ollama to generate short feedback. The mock provider returns the same
example Two Sum submission for any event; replace it with Submission Service
and Catalog clients in a later part. Feedback is currently logged and is not
yet persisted or returned through a user-facing API.

## RabbitMQ contract

The Judge Worker publishes persistent messages to the durable `verdicts` topic
exchange using `verdict.<status>` routing keys. This service declares its own
durable `ai.feedback.jobs` queue and binds only:

- `verdict.wrong_answer`
- `verdict.time_limit_exceeded`
- `verdict.memory_limit_exceeded`
- `verdict.runtime_error`
- `verdict.compile_error`

Accepted verdicts are not consumed. The event is validated against the Judge
Worker's fields: `submission_id`, `user_id`, `problem_id`, `testset_version`,
`verdict`, `rejudge_seq`, and `judged_at`. Extra verdict fields are ignored.

Malformed or mismatched events are dead-lettered to `ai.feedback.dlq`. Valid
events are manually acknowledged after the handler completes. RabbitMQ can
redeliver if the consumer fails before acknowledgement, so later handlers that
call services or store results must be idempotent. Transient processing errors
are currently requeued; bounded retry/dead-letter policy for repeated transient
failures is a follow-up reliability part.

## Run locally

Start RabbitMQ from the repository root, then:

```bash
cd services/ai-service
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/python -m app.main
```

The consumer uses `AI_`-prefixed environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `AI_RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | Broker connection |
| `AI_VERDICTS_EXCHANGE` | `verdicts` | Judge Worker topic exchange |
| `AI_FEEDBACK_QUEUE` | `ai.feedback.jobs` | AI-owned durable queue |
| `AI_DEAD_LETTER_EXCHANGE` | `verdicts.dlx` | Direct exchange for invalid messages |
| `AI_DEAD_LETTER_QUEUE` | `ai.feedback.dlq` | Queue for invalid messages |
| `AI_RECONNECT_MAX_SECONDS` | `30` | Maximum reconnect delay |
| `AI_OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama server URL |
| `AI_OLLAMA_MODEL` | `llama3.2` | Local model name; it must already be available in Ollama |
| `AI_OLLAMA_TIMEOUT_SECONDS` | `120` | HTTP timeout for a generation request |

The mock handler logs the generated feedback. It does not yet persist feedback
or expose it through a user-facing API. Since the message is acknowledged after
generation/logging, a crash before acknowledgement may cause Ollama to generate
the same response again; durable idempotency is a later reliability part.
