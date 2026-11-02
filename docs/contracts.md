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
| `verdicts` | topic exchange | Submission status events, copied to every queue whose binding matches. |
| `submission-service.verdict.queue` | quorum queue, bound to `verdicts` on `#` | Submission Service writes events to its DB. |
| `catalog.verdicts` | classic queue, bound to `verdicts` on `verdict.#` | Catalog updates solves and the leaderboard. |
| `submission-service.verdict.dlq`, `jobs.dlq` + `dead-letter` exchange | | Rejected or over-delivered messages, for inspection. |
| `catalog.verdicts.dlq` + `verdicts.dlx` exchange | | The same for Catalog. |

Routing keys on `verdicts`:

| Key | Sent by | Received by |
|---|---|---|
| `verdict.<verdict>`, lowercase (`verdict.accepted`) | Judge Worker, finished jobs | Submission Service, Catalog |
| `status.running` | Judge Worker, started jobs | Submission Service |
| `status.update` | Reaper and fake worker | Submission Service |

The Submission Service also creates one **exclusive, auto-delete** queue per running instance, bound to `verdicts` on `#`. It feeds that instance's live SSE connections, so the browser gets updates whichever instance it is connected to.

All messages are persistent (`delivery_mode=2`), `content_type=application/json`, with `message_id` set to the `submission_id`.

Delivery is **at-least-once**: any consumer can see the same message twice and must not double-count.

## 2. Job message: Submission Service → Judge Worker

Queue `jobs.queue`. Published by the Submission Service outbox relay.

```json
{
  "submission_id": "dc7626b3-f4e7-4194-a625-300911712913",
  "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "problem_id": "11111111-1111-1111-1111-111111111111",
  "language": "python",
  "code": "class Solution:\n    def twoSum(self, nums, target):\n        return [0, 1]",
  "submitted_at": "2026-10-25T11:00:00Z"
}
```

- `problem_id` is the Catalog problem **UUID** (the `id` from Catalog's problem responses), here and in every message and API below.
- `language` is currently only `python`.
- `code` is at most 64 KB.

Worker rules:
- Manual ack, `prefetch=1`.
- Ack the job only **after** the final event below has been published (with publisher confirms).
- If the worker crashes mid-job, RabbitMQ redelivers it. After 3 deliveries the job goes to `jobs.dlq`, and the Submission Service's reaper eventually marks the submission `INTERNAL_ERROR`.

## 3. Status events: Judge Worker → `verdicts`

The worker publishes **two** events per job.

**a) When it starts running the job:**
```json
{
  "submission_id": "dc7626b3-f4e7-4194-a625-300911712913",
  "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "problem_id": "11111111-1111-1111-1111-111111111111",
  "status": "RUNNING"
}
```

**b) When it finishes:**
```json
{
  "submission_id": "dc7626b3-f4e7-4194-a625-300911712913",
  "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "problem_id": "11111111-1111-1111-1111-111111111111",
  "status": "COMPLETED",
  "verdict": "ACCEPTED",
  "execution_time_ms": 38,
  "memory_kb": 14200,
  "test_cases_passed": 55,
  "total_test_cases": 55,
  "error_message": null,
  "testset_version": 3,
  "rejudge_seq": 0,
  "judged_at": "2026-10-25T11:00:02Z"
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
- `testset_version`, `rejudge_seq`, `judged_at`: required by Catalog. `rejudge_seq` stays `0` until re-judging is built.

The **Submission Service also publishes** a `COMPLETED` / `INTERNAL_ERROR` event to `verdicts`, with routing key `status.update`, when its reaper gives up on a submission the judge never finished:
- default timeouts: 30 min in `QUEUED`, 5 min in `RUNNING`
- `error_message`: "The judge did not finish this submission in time. Please submit again."

A late verdict from the judge for that submission is then ignored by the Submission Service.

Consumer rules:
- **Catalog** only receives `verdict.*` messages, so it never sees `RUNNING` events or the reaper's `INTERNAL_ERROR`. A redelivered verdict must not count twice (it compares `rejudge_seq` per submission).
- **Submission Service** applies events as a one-way state machine, `QUEUED → RUNNING → COMPLETED`. Late or duplicate events are no-ops.

## 4. Submission Service HTTP API

All routes go through the gateway, which sets `X-User-Id`.

### `POST /api/v1/submissions` → `202 Accepted`
Request:
```json
{ "problem_id": "11111111-1111-1111-1111-111111111111", "language": "python", "code": "..." }
```
Response (also `Location: /api/v1/submissions/{id}`):
```json
{ "submission_id": "dc7626b3-...", "status": "QUEUED", "created_at": "2026-10-25T11:00:00Z" }
```
The submitting user is always the `X-User-Id` caller; a `user_id` in the body is ignored.

Errors (same codes as Catalog):
- `400 bad_request`: malformed JSON
- `401 unauthorized`: missing or invalid `X-User-Id`
- `413 payload_too_large`: code over 64 KB
- `422 validation_failed`: `problem_id` is not a UUID, unknown `language`, or empty `code`. The message lists every problem.

Visibility: users see only their own submissions; `X-User-Role: admin` sees all. Someone else's submission answers `404`, not `403`, so IDs cannot be probed.

### `GET /api/v1/submissions/{id}` → `200 OK`
```json
{
  "submission_id": "dc7626b3-...",
  "user_id": "aaaaaaaa-...",
  "problem_id": "11111111-1111-1111-1111-111111111111",
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

### `GET /api/v1/submissions?problem_id=11111111-1111-1111-1111-111111111111&limit=20&cursor=...` → `200 OK`
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
