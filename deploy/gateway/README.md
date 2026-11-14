# Gateway (Envoy)

The only entry point from outside. Configuration: [envoy.yaml](envoy.yaml). Token rules: section 7 of
[docs/contracts.md](../../docs/contracts.md).

For every request the gateway:
1. removes any `X-User-Id`, `X-User-Role` and `X-Session-Id` the client sent;
2. verifies the access token (ES256) with the User Service's public keys, which it caches for 5 minutes;
3. sets those headers from the token's `sub`, `role` and `sid`;
4. routes the request to the service that owns it.

No request calls the User Service to be authorised. While the User Service is down, signed-in users keep
working; only signing in and refreshing fail.

## Routes

| Path | Service | Token |
|---|---|---|
| `/auth/*` | User Service | none; rate limited to 20 requests a minute per client address |
| `/.well-known/jwks.json` | User Service | none |
| `/users*` | User Service | required |
| `/api/v1/submissions*`, `/api/v1/runs*` | Submission Service | required; the live-update stream has no timeout |
| `/api/v1/ai/*` | AI Service (prefix removed) | required |
| `GET /problems*`, `GET /leaderboard*` | Catalog | optional, but checked if sent |
| `/admin/*` | Catalog (which checks the admin role) | required |
| `/internal/*` | never routed | `404` |
| `/gateway/healthz` | the gateway itself | none |

Errors the gateway makes itself (`401`, `403`, `429`, `503`) use the same `{"error", "message"}` body as the
services.

## Run it

```powershell
docker compose up -d --wait gateway
```

The gateway listens on **http://localhost:8000**:

```bash
curl -X POST localhost:8000/auth/login -H 'Content-Type: application/json' \
     -d '{"email":"admin@example.com","password":"admin-local-only"}'
curl localhost:8000/users/me -H "Authorization: Bearer <access_token>"
```

Check a configuration change before restarting:

```bash
docker run --rm -v "$PWD/deploy/gateway/envoy.yaml:/etc/envoy/envoy.yaml:ro" envoyproxy/envoy:v1.39.6 \
       envoy --mode validate -c /etc/envoy/envoy.yaml
```

## Notes
- **Direct service ports** (8080-8085) are published for local debugging only. Behind them, any client could
  send its own `X-User-Id`. In a real deployment only the gateway is exposed.
- **Rate limits** are per gateway instance. With several instances, each client gets the limit on each one.
- **Client address:** the gateway appends the address it saw as the last `X-Forwarded-For` entry. Services must
  read the last entry, never the first, which the client controls. Behind another proxy (such as an OpenShift
  router), raise `xff_num_trusted_hops` to match.
