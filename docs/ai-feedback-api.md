# AI Feedback API

## Request feedback

```http
POST /submissions/{submission_id}/feedback
X-User-ID: user-42
Idempotency-Key: feedback-attempt-1
```

No body is required. Generates and stores feedback, then returns the request and
result. Generation is synchronous in this version. Requires `X-User-ID` and an
`Idempotency-Key`. Returns `503` with an `unavailable` request record if Ollama
fails.

## Get a feedback request

```http
GET /feedback-requests/{request_id}
X-User-ID: user-42
```

Returns the request status and saved feedback, if ready.

## Get the latest feedback for a submission

```http
GET /submissions/{submission_id}/feedback
X-User-ID: user-42
```

All routes require the trusted user identity set by the gateway. Swagger UI is
served at `/docs`. Submission and problem details are mocked in this increment.
Feedback is stored in the AI service's PostgreSQL database.
