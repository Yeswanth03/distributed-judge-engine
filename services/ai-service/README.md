# AI Service: failed-verdict consumer

Part 1 of the AI feedback service. It consumes failed verdict events from the
Judge Worker's RabbitMQ topic exchange and extracts the submission identifier.
Submission lookup and Ollama integration are later parts.

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

At this stage the handler logs the IDs from valid failed-verdict events. It does
not yet fetch source/problem details, call Ollama, or save feedback. Do not treat
this part as the completed AI feedback pipeline.
