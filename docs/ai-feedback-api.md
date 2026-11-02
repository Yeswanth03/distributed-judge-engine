# AI Feedback API

## Request feedback

```http
POST /submissions/{submission_id}/feedback
X-User-ID: user-42
Idempotency-Key: feedback-attempt-1
```

Body includes `verdict`, `language`, `source_code`, and safe `problem` details
(`problem_id`, `title`, `statement`, `constraints`). Generates and stores
feedback, then returns the result. Generation is synchronous. Returns `503`
with an `unavailable` request record if Ollama fails.

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
served at `/docs`. The body is for Swagger/local development; production should
fetch source and problem details server-to-server. Failed verdict events are
stored as `awaiting_context` until the Submission Service lookup is integrated.
Feedback is stored in the AI service's PostgreSQL database.
