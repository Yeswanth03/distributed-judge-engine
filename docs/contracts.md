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
| `runs.queue` | quorum queue, `x-delivery-limit: 3` | Trial runs for the Judge Worker (section 6). Separate from `jobs.queue`, so a worker that predates runs can never score one. |
| `submission-service.run-results.queue` | quorum queue, `x-delivery-limit: 5` | Run results, sent by the worker through the default exchange. Never on `verdicts`, so runs reach neither Catalog nor the AI Service. |
| `runs.dlq`, `submission-service.run-results.dlq` | quorum queues, via `dead-letter` | The same for runs. |

Routing keys on `verdicts`:

| Key | Sent by | Received by |
|---|---|---|
| `verdict.<verdict>`, lowercase (`verdict.accepted`) | Judge Worker, finished jobs | Submission Service, Catalog |
| `status.running` | Judge Worker, started jobs | Submission Service |
| `status.update` | Reaper and fake worker | Submission Service |

The Submission Service also creates one **exclusive, auto-delete** queue per running instance, bound to `verdicts` on `#`. It feeds that instance's live SSE connections, so the browser gets updates whichever instance it is connected to.

All messages are persistent (`delivery_mode=2`), `content_type=application/json`, with `message_id` set to the `submission_id` (the `run_id` for runs).

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

Problems are functions to implement. `GET /internal/problems/{id}/testset` returns:

```json
{
  "problem_id": "a68b526a-ed8b-55f7-8113-80baab531c65",
  "version": 2,
  "content_hash": "...",
  "time_limit_ms": 1000,
  "memory_limit_mb": 256,
  "function_name": "twoSum",
  "parameters": [{ "name": "nums", "type": "int[]" }, { "name": "target", "type": "int" }],
  "return_type": "int[]",
  "cases": [
    { "ordinal": 1, "input": "{\"nums\":[3,8,12,5],\"target\":17}", "expected_output": "[2,3]", "is_sample": true }
  ]
}
```

- `input` and `expected_output` are **strings holding compact JSON**. `input` is an object with one value per parameter; `expected_output` is a value of `return_type`.
- Types are `int`, `float`, `bool` or `string`, optionally followed by `[]` or `[][]`.
- The worker runs a harness around the submission: read `input`, call `Solution().<function_name>(**args)`, print the result as JSON. Compare **parsed** values, not text. Allow a small tolerance when `return_type` contains `float`.
- `function_name`, `parameters` and `return_type` are fixed once a problem is published. They are `null` only for a problem created before function details existed.

The frontend gets `starter_code` from `GET /problems/{slug}`; its `samples` use the same JSON strings.

## 6. Trial runs ("Run" in the editor)

A run executes code against the problem's **sample cases**, or against the caller's own inputs, and shows everything: return values, the code's own prints and the full error output. It is never scored, never reaches Catalog or the AI Service, and is deleted after a day (`RUN_RETENTION`). Hidden test cases are never executed by a run.

### `POST /api/v1/runs` → `202 Accepted`
```json
{ "problem_id": "11111111-1111-1111-1111-111111111111", "language": "python", "code": "...", "inputs": ["{\"nums\":[2,7],\"target\":9}"] }
```
- `inputs` is optional. Without it the samples run; with it, only these inputs run (at most 10, together at most `MAX_CODE_BYTES`). For a function problem each input is a JSON object with one value per parameter, like a case's `input`.
- Response (also `Location: /api/v1/runs/{id}`): `{ "run_id": "...", "status": "QUEUED", "created_at": "..." }`.
- Same validation and errors as a submission, plus `429 too_many_requests` (with `Retry-After`) above `RUNS_PER_MINUTE` runs per user per minute.

### `GET /api/v1/runs/{id}` → `200 OK`
Poll until `status` is `COMPLETED` or `FAILED` (`FAILED` means the judge never answered within `RUN_TIMEOUT`; `error_message` says so). Someone else's run answers `404`.
```json
{
  "run_id": "...", "problem_id": "...", "language": "python",
  "status": "COMPLETED", "error_message": null,
  "result": {
    "run_id": "...", "status": "COMPLETED", "verdict": "WRONG_ANSWER", "finished_at": "...",
    "cases": [
      { "kind": "sample", "input": "{\"nums\":[3,2,4],\"target\":6}", "expected_output": "[1,2]",
        "output": "[0,2]", "stdout": "checking 3
", "error": null, "verdict": "WRONG_ANSWER",
        "time_ms": 41, "memory_kb": 9216 }
    ]
  },
  "created_at": "...", "updated_at": "..."
}
```
- `result.verdict` covers the cases whose expected output is known; it is `null` when there are none.
- Each case's `verdict` is `ACCEPTED` or `WRONG_ANSWER` when the expected output is known, `FINISHED` when it isn't (a custom input; there is no reference solution), otherwise `TIME_LIMIT_EXCEEDED`, `MEMORY_LIMIT_EXCEEDED`, `RUNTIME_ERROR` or `INVALID_INPUT` (the input isn't a JSON object; nothing ran).
- A custom input that equals a sample's input gets that sample's `expected_output`.
- `output` is the return value as JSON (stdout for a legacy stdin/stdout problem), `null` if the code crashed. `stdout` is the code's own prints, kept apart from the answer. `stdout`, `output` and `error` are cut off at 10,000 characters.

### Messages
- **Job** (Submission Service → `runs.queue`, through the outbox, event type `RUN_REQUESTED`): `{ "run_id", "user_id", "problem_id", "language", "code", "inputs": [...] | null, "requested_at" }`.
- **Result** (Judge Worker → `submission-service.run-results.queue`): the `result` object above. The worker acks the job only after the result is confirmed. The Submission Service stores the first result per run; duplicates are ignored.

## 7. User Service: identity, sessions and roles

The User Service owns accounts, credentials, roles and sessions. It does **not** own submission history (Submission Service) or scores (Catalog): a profile page reads those from their owners.

### Access tokens: what the gateway verifies
Login returns a short-lived **access token**: a JWT signed with **ES256**. The gateway verifies it locally with the public keys from `GET /.well-known/jwks.json` (cache them for 5 minutes; pick the key by `kid`), so no request has to call the User Service to be authorised.

| Claim | Meaning |
|---|---|
| `sub` | User ID (UUID) |
| `role` | `user` or `admin` |
| `sid` | Session ID |
| `iss` | `judge-engine-user-service` |
| `aud` | `judge-engine` |
| `exp`, `iat`, `nbf`, `jti` | Standard; lifetime `ACCESS_TOKEN_TTL` (default 10 minutes) |

The gateway must:
- reject a request with an invalid or expired token (`401`), except on public routes (`/auth/*`, `/.well-known/jwks.json`, public Catalog reads);
- **remove** any `X-User-Id`, `X-User-Role` and `X-Session-Id` the client sent, then set them from `sub`, `role` and `sid`;
- never route `/internal/*`;
- append the client address it saw as the **last** `X-Forwarded-For` entry. Services read the last entry, never the first, which the client controls.

The gateway is Envoy, configured in [deploy/gateway/envoy.yaml](../deploy/gateway/envoy.yaml); its route map is in [deploy/gateway/README.md](../deploy/gateway/README.md).

An access token can't be revoked: a role change, a disabled account or a signed-out session takes effect within `ACCESS_TOKEN_TTL` at the latest.

### Refresh tokens and sessions
The **refresh token** is opaque and long-lived (`SESSION_TTL`, default 30 days). Each one works **once**: `POST /auth/refresh` returns a new access token and a new refresh token. Presenting a refresh token that was already used means it was copied, so the whole session is revoked. Each new access token carries the user's current role.

**Browsers get the refresh token as a cookie, never in the body.** A client that sends `X-Token-Transport: cookie` on `/auth/register`, `/auth/login`, `/auth/oauth/exchange`, `/auth/refresh` and `/auth/logout`:
- gets the refresh token in a cookie `refresh_token` (`HttpOnly`, `SameSite=Strict`, `Path=/auth`, `Max-Age` = `refresh_expires_in`, `Secure` outside the local profile, see `REFRESH_COOKIE_SECURE`), and **no** `refresh_token` field in the JSON;
- refreshes and signs out with an empty body `{}`: the token comes from the cookie;
- has the cookie cleared when a refresh is refused and on logout.

Without that header nothing changes: the token comes and goes in the JSON body (scripts, tests, curl). A cookie without the header is ignored. A proxy that serves these routes under another prefix rewrites the cookie path to match (the frontend serves them under `/api/auth`).

### HTTP API
| Route | Who | Purpose |
|---|---|---|
| `POST /auth/register` | anyone | `{email, username, password, display_name?}` → `201` with tokens and the user. `409 email_taken` / `username_taken`, `422 validation_failed` |
| `POST /auth/login` | anyone | `{email, password}` → `200` with tokens. Every failure is the same `401 invalid_credentials`; a disabled account with the right password gets `403 account_disabled`. Locked for `LOGIN_LOCKOUT` after `LOGIN_MAX_FAILURES` wrong passwords in a row |
| `POST /auth/refresh` | anyone | `{refresh_token}` → `200` with new tokens, or `401 invalid_refresh_token` |
| `POST /auth/logout` | anyone | `{refresh_token}` → `204`, always |
| `GET /.well-known/jwks.json` | anyone | Public keys (`Cache-Control: max-age=300`) |
| `GET /users/me`, `PATCH /users/me` | signed in | Own profile; `PATCH` takes `{display_name}` |
| `GET /users/me/sessions`, `DELETE /users/me/sessions/{id}` | signed in | Own devices; `current: true` marks the request's own session |
| `GET /users?limit=&cursor=` | admin | All accounts, newest first |
| `PATCH /users/{id}/role`, `PATCH /users/{id}/status` | admin | `{"role": "user"}` or `"admin"`; `{"status": "active"}` or `"disabled"`. Disabling revokes every session. Admins can't change their own role or status (`409 cannot_change_self`) |

Token responses: `{access_token, token_type: "Bearer", expires_in, refresh_token, refresh_expires_in, user?}`.

### Sign in with Google or GitHub
The authorization code flow with PKCE, run on the server: client secrets never reach a browser, and tokens never travel in a URL.

1. The page sends the browser to `GET /auth/oauth/{google|github}/start?redirect_uri=<page>`. `<page>`'s origin must be in `OAUTH_ALLOWED_REDIRECTS`, otherwise `400`. `GET /auth/oauth/providers` lists the enabled providers.
2. The provider sends the browser back to `/auth/oauth/{provider}/callback`. The User Service checks the single-use `state` (10 minutes), exchanges the code with its PKCE verifier and reads the account.
3. The browser lands on `<page>?login_code=...` (single use, 1 minute), or `<page>?login_error=...` with one of `cancelled`, `provider_error`, `email_not_verified`, `email_in_use`, `account_disabled`.
4. The page calls `POST /auth/oauth/exchange` `{login_code}` and gets the usual token response.

Which account signs in:
- the account already linked to that Google or GitHub account;
- else the account with the same email, **only if** the provider verified it **and** the account's email is verified too (it was created through a provider). A password account is never linked by email: its email isn't verified, so anyone could take it over by signing up at a provider with that address. That case answers `email_in_use`: sign in with the password instead;
- else a new account with no password, `email_verified: true`, and a username taken from the provider (with a numeric suffix if taken).

### Events: User Service → `users` exchange
A topic exchange; the routing key is the event type. Published through an outbox, at least once.

| Routing key | When |
|---|---|
| `user.registered` | An account was created |
| `user.updated` | The display name changed |
| `user.role_changed` | An admin changed the role |
| `user.status_changed` | An admin enabled or disabled the account |

```json
{
  "event_id": "...", "type": "user.updated", "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "username": "ada", "display_name": "Ada L.", "role": "user", "status": "active",
  "version": 3, "occurred_at": "2026-11-11T12:00:00Z"
}
```

- Every event carries the user's **full current public state**, so a consumer can simply upsert it.
- `version` rises with every change. Keep the highest version seen per `user_id` and ignore anything older: that makes duplicates and out-of-order deliveries harmless.
- Events never carry the email address or anything about credentials.
- Consumers declare and bind their own queue (for example `catalog.users` on `user.#`).
