# Service Contracts

How the Submission Service, Judge Worker, Catalog and frontend talk to each other. If you change anything here, update this file in the same PR and tell the owners of the other side.

General rules (same as Catalog):
- JSON field names are `snake_case`.
- Errors are `{"error": "<code>", "message": "<human text>"}`.
- User identity comes from the gateway: it verifies the login token and forwards `X-User-Id` (UUID) and `X-User-Role`. Services trust these headers only because they are reachable through the gateway alone.
- Service-to-service HTTP calls send `X-Service-Token`.

## 1. RabbitMQ topology

Declared once in [deploy/rabbitmq/definitions.json](../deploy/rabbitmq/definitions.json). Services may re-declare a queue with **exactly** these arguments, or just use it. Different arguments fail with `PRECONDITION_FAILED`.

| Name | Kind | Purpose |
|---|---|---|
| `jobs.queue` | quorum queue, `x-delivery-limit: 3` | Jobs for the Judge Worker. Published through the default exchange with routing key `jobs.queue`. |
| `jobs.dlq` | quorum queue | Jobs that were redelivered more than 3 times (crashing or poison jobs). |
| `verdict.fanout` | fanout exchange | Submission status events from the worker, copied to every bound queue. |
| `submission-service.verdict.queue` | quorum queue, bound to `verdict.fanout` | Submission Service writes events to its DB. |
| `catalog-service.verdict.queue` | quorum queue, bound to `verdict.fanout` | Catalog updates solves and the leaderboard. |
| `*.dlq` + `dead-letter` exchange | | Rejected or over-delivered messages, for inspection. |

The Submission Service also creates one **exclusive, auto-delete** queue per running instance, bound to `verdict.fanout`. It feeds that instance's live SSE connections, so the browser gets updates whichever instance it is connected to.

All messages are persistent (`delivery_mode=2`), `content_type=application/json`, with `message_id` set to the `submission_id`.

Delivery is **at-least-once**: any consumer can see the same message twice and must not double-count.

## 2. Job message: Submission Service → Judge Worker

Queue `jobs.queue`. Published by the Submission Service outbox relay.

```json
{
  "submission_id": "dc7626b3-f4e7-4194-a625-300911712913",
  "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "problem_id": "two-sum",
  "language": "python",
  "code": "class Solution:\n    def twoSum(self, nums, target):\n        return [0, 1]",
  "submitted_at": "2026-10-25T11:00:00Z"
}
```

- `problem_id` is the Catalog **slug**.
- `language` is currently only `python`.
- `code` is at most 64 KB.

Worker rules:
- Manual ack, `prefetch=1`.
- Ack the job only **after** the final event below has been published (with publisher confirms).
- If the worker crashes mid-job, RabbitMQ redelivers it. After 3 deliveries the job goes to `jobs.dlq`, and the Submission Service's reaper eventually marks the submission `INTERNAL_ERROR`.

## 3. Status events: Judge Worker → `verdict.fanout`

The worker publishes **two** events per job.

**a) When it starts running the job:**
```json
{
  "submission_id": "dc7626b3-f4e7-4194-a625-300911712913",
  "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "problem_id": "two-sum",
  "status": "RUNNING"
}
```

**b) When it finishes:**
```json
{
  "submission_id": "dc7626b3-f4e7-4194-a625-300911712913",
  "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "problem_id": "two-sum",
  "status": "COMPLETED",
  "verdict": "ACCEPTED",
  "execution_time_ms": 38,
  "memory_kb": 14200,
  "test_cases_passed": 55,
  "total_test_cases": 55,
  "error_message": null,
  "testset_version": 3
}
```

- `status` is one of `RUNNING` or `COMPLETED`.
- `verdict` is one of:
  - `ACCEPTED`
  - `WRONG_ANSWER`
  - `TIME_LIMIT_EXCEEDED`
  - `MEMORY_LIMIT_EXCEEDED`
  - `RUNTIME_ERROR`
  - `COMPILATION_ERROR`
  - `INTERNAL_ERROR` (judge failure, not the user's fault)
- `error_message`: short text for RE/CE/IE, otherwise `null`. Never put hidden test case data in it.
- `testset_version`: the Catalog test set version that was judged (optional but recommended).

Consumer rules:
- **Catalog** ignores every event whose `status` is not `COMPLETED`. Dedupe by `submission_id` (for example a `processed_events` table) so a redelivered verdict does not count twice.
- **Submission Service** applies events as a one-way state machine, `QUEUED → RUNNING → COMPLETED`. Late or duplicate events are no-ops.

## 4. Submission Service HTTP API

All routes go through the gateway, which sets `X-User-Id`.

### `POST /api/v1/submissions` → `202 Accepted`
Request:
```json
{ "problem_id": "two-sum", "language": "python", "code": "..." }
```
Response (also `Location: /api/v1/submissions/{id}`):
```json
{ "submission_id": "dc7626b3-...", "status": "QUEUED", "created_at": "2026-10-25T11:00:00Z" }
```
Errors:
- `400 bad_request`: invalid JSON, unknown language, empty code
- `401 unauthorized`: missing or invalid `X-User-Id`
- `413 payload_too_large`: code over 64 KB

### `GET /api/v1/submissions/{id}` → `200 OK`
```json
{
  "submission_id": "dc7626b3-...",
  "user_id": "aaaaaaaa-...",
  "problem_id": "two-sum",
  "language": "python",
  "status": "COMPLETED",
  "verdict": "ACCEPTED",
  "execution_time_ms": 38,
  "memory_kb": 14200,
  "test_cases_passed": 55,
  "total_test_cases": 55,
  "error_message": null,
  "created_at": "2026-10-25T11:00:00Z",
  "updated_at": "2026-10-25T11:00:02Z"
}
```
- Add `?include=code` to also get `code`.
- Returns `404 not_found` if the submission doesn't exist.

### `GET /api/v1/submissions?problem_id=two-sum&limit=20&cursor=...` → `200 OK`
The caller's own submissions, newest first, in the same shape as above:
```json
{ "items": [ ... ], "next_cursor": "..." }
```
`next_cursor` is `null` on the last page.

### `GET /api/v1/submissions/{id}/events` (Server-Sent Events)
The current state is sent first, then live changes. The stream closes after the `verdict` event.
```text
event: status
data: {"submission_id":"dc7626b3-...","status":"QUEUED"}

event: status
data: {"submission_id":"dc7626b3-...","status":"RUNNING"}

event: verdict
data: {"submission_id":"dc7626b3-...","status":"COMPLETED","verdict":"ACCEPTED","execution_time_ms":38,"memory_kb":14200,"test_cases_passed":55,"total_test_cases":55,"error_message":null}
```
A `: ping` comment is sent every 15 s to keep proxies from closing the connection.

Frontend: use `EventSource`, and call `close()` after the `verdict` event. Otherwise `EventSource` reconnects automatically and you get the state again, which is harmless.

### Probes
- `GET /healthz`: liveness.
- `GET /readyz`: readiness (DB and broker reachable).
- `GET /metrics`: Prometheus.

## 5. Judge Worker → Catalog

The worker fetches hidden test cases from Catalog's `/internal/...` endpoint with `X-Service-Token` (owned by the Catalog team, see their Swagger at `/swagger-ui.html`). Published test sets are immutable, so the worker can cache them by `(problem, testset_version)`.
