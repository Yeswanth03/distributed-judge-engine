# AI Feedback API

## Fetch submitted code (internal Submission Service)

```http
GET /internal/submissions/{submission_id}/source
X-Service-Token: <SERVICE_TOKEN>
```

Returns `submission_id`, `user_id`, `problem_id`, `language`, and `source_code`.

## Automatic feedback for failed verdicts (RabbitMQ)

The AI Service consumes `verdict.wrong_answer`, `verdict.time_limit_exceeded`,
`verdict.memory_limit_exceeded`, `verdict.runtime_error`, and
`verdict.compile_error`. It fetches the source using `submission_id` and
generates feedback. It does not consume `verdict.accepted` automatically.

## Request feedback (AI Service)

```http
POST /submissions/{submission_id}/feedback
Authorization: Bearer <USER_TOKEN>
```

Use for on-demand feedback, including accepted submissions. Returns `202`
with a `request_id` and `status: queued`.

## Get feedback (AI Service)

```http
GET /feedback-requests/{request_id}
Authorization: Bearer <USER_TOKEN>
```

Returns `status` (`queued`, `processing`, `ready`, or `unavailable`) and the
feedback when ready.
