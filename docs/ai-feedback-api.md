# AI Feedback API

Full details: [services/ai-service/README.md](../services/ai-service/README.md).

## Automatic feedback for failed verdicts (RabbitMQ)

The AI Service consumes `verdict.wrong_answer`, `verdict.time_limit_exceeded`,
`verdict.memory_limit_exceeded`, `verdict.runtime_error` and
`verdict.compilation_error` from the `verdicts` exchange. For each one it:

1. fetches the code by `submission_id` from the Submission Service
   (`GET /api/v1/submissions/{id}?include=code`);
2. fetches the problem statement from Catalog (`GET /problems/{slug}`);
3. fetches the test cases from Catalog (`GET /internal/problems/{uuid}/testset`).

It then generates feedback and saves it. It does not consume `verdict.accepted`.
Only sample test cases are shown to the model.

## Request feedback

```http
POST /submissions/{submission_id}/feedback
X-User-Id: <set by the gateway>
```

There is no body. It works for any final verdict, including accepted. The response
is one of:
- `202`: the request is `queued` (or `processing`)
- `200`: feedback is already `ready`
- `404`: the submission is unknown or not the caller's
- `409`: the submission is not judged yet

## Get feedback

```http
GET /feedback-requests/{request_id}
GET /submissions/{submission_id}/feedback
X-User-Id: <set by the gateway>
```

The response has `status` (`queued`, `processing`, `ready` or `unavailable`), plus
the feedback when it is ready, or `error_reason` when it is unavailable.
