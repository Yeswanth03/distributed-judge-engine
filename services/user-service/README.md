# User Service

Accounts, login and roles for the judge. It issues the access tokens the gateway verifies, keeps each
signed-in device as a session, and publishes user events for other services. It does not own submission
history (Submission Service) or scores (Catalog).

The API, token claims and events are in [section 7 of docs/contracts.md](../../docs/contracts.md).

## How it works

```
POST /auth/login ──► check password (argon2id) ──► new session + refresh token (hashed) ──► access token (ES256 JWT)

every request ──► gateway verifies the JWT with the cached JWKS ──► sets X-User-Id / X-User-Role ──► any service
                  (no call to this service)

changes ──► users table + outbox_events (one transaction) ──► relay ──► "users" exchange ──► consumers
```

| Concern | How it's handled | Code |
|---|---|---|
| Passwords | argon2id (OWASP parameters), PHC format so costs can be raised later; NIST length rules | [password](internal/password/password.go) |
| Guessing | Same answer and timing for unknown email, wrong password and locked account; lockout after repeated failures | [auth.go](internal/handler/auth.go) |
| Stolen refresh token | Each refresh token works once; a reused one revokes the whole session | [sessions.go](internal/repository/sessions.go) |
| Availability | The gateway verifies tokens on its own, so this service is never on another request's critical path | [token](internal/token/token.go) |
| Key rotation | Keys are identified by thumbprint (`kid`); old public keys stay in the JWKS until their tokens expire | [token](internal/token/token.go) |
| Google / GitHub | Authorization code flow with PKCE on the server; single-use state and login codes; never links a password account by email | [oauth](internal/oauth/oauth.go), [oauth.go](internal/handler/oauth.go) |
| Events | Transactional outbox, publisher confirms, a `version` per user so consumers can drop stale events | [outbox](internal/outbox/relay.go) |

## Run it

From the repository root (Docker Desktop must be running):

```powershell
docker compose up -d --build --wait user-service
```

The service listens on **http://localhost:8085**. A first admin is created on start:
`admin@example.com` / `admin-local-only` (local only; set `BOOTSTRAP_ADMIN_PASSWORD` to change it).

Until the gateway exists, signed-in routes need the identity headers the gateway will set:

```bash
curl -X POST localhost:8085/auth/register -H 'Content-Type: application/json' \
     -d '{"email":"ada@example.com","username":"ada","password":"correct horse"}'
curl localhost:8085/users/me -H "X-User-Id: <sub from the access token>"
```

## Configuration

| Variable | Default (local) | |
|---|---|---|
| `APP_PROFILE` | `local` | `openshift` requires the connection settings and a signing key |
| `DATABASE_URL` | `postgres://users:users@localhost:5432/users` | |
| `AMQP_URL` | `amqp://judge:judge@localhost:5672/` | |
| `JWT_PRIVATE_KEY` / `JWT_PRIVATE_KEY_FILE` | none | ECDSA P-256 key, PEM. Local profile without one: a key stored in the database |
| `JWT_PREVIOUS_PUBLIC_KEYS` / `_FILE` | none | Public keys of earlier signing keys, during a rotation |
| `JWT_ISSUER`, `JWT_AUDIENCE` | `judge-engine-user-service`, `judge-engine` | |
| `ACCESS_TOKEN_TTL` | `10m` | At most `1h`: access tokens can't be revoked |
| `SESSION_TTL` | `720h` (30 days) | Refresh token lifetime |
| `LOGIN_MAX_FAILURES`, `LOGIN_LOCKOUT` | `5`, `15m` | |
| `BOOTSTRAP_ADMIN_EMAIL`, `_USERNAME`, `_PASSWORD` | none | First admin, created only while there is no admin |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | none | Sign in with Google; enabled when both are set. Callback: `PUBLIC_BASE_URL/auth/oauth/google/callback` |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | none | Sign in with GitHub; same rule. Callback: `PUBLIC_BASE_URL/auth/oauth/github/callback` |
| `PUBLIC_BASE_URL` | `http://localhost:8000` | Where browsers reach the gateway |
| `OAUTH_ALLOWED_REDIRECTS` | `http://localhost:5500,http://localhost:8000` | Origins a sign-in may return to |
| `CORS_ORIGIN` | empty | |
| `OUTBOX_*`, `SHUTDOWN_TIMEOUT`, `HTTP_ADDR`, `LOG_LEVEL` | as in the Submission Service | |

Making a signing key: `openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out jwt.pem`

## Tests

```powershell
$env:TEST_DATABASE_URL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
go test ./...
```

Repository and HTTP tests run against a real PostgreSQL, each in its own throwaway database. Without
`TEST_DATABASE_URL` they are skipped when no database is reachable; with it they fail instead.
