# Spawner

The only service allowed to talk to the Docker daemon. Owns the container pool: 1
container always open (the floor, respawned immediately if it dies), everything
else spawned on demand and reclaimed after 5 minutes idle. Judge Worker never
touches Docker directly, it only calls this service's `/execute` endpoint.

Full design rationale, the three options considered, and the complete security
checklist live in the project's design doc (ask Sharon for the link if you don't
have it) and in `docs/` at the repo root once that's added.

## Running locally

Needs Docker running on the host (it creates/destroys containers via the Docker
socket).

```
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/uvicorn app.main:app --reload --port 8081
```

## Tests

```
.venv/bin/python -m pytest tests/ -v
```
These run without a real Docker daemon, a fake container stands in so the pool's
claim/release/reaper/floor-respawn logic can be tested in isolation.

## Config (env vars, prefix `SPAWNER_`)

| Var | Default | Meaning |
|---|---|---|
| `SPAWNER_SANDBOX_IMAGE` | `python:3.11-slim` | image every pool container runs |
| `SPAWNER_RETENTION_SECONDS` | `300` (5 min) | idle time before a non-floor container is reclaimed |
| `SPAWNER_MAX_CONTAINERS` | `10` | **placeholder, not yet sized for a real host.** Compute from `(memory reserved for sandboxes) / (memory_limit_mb per submission)` before using this for anything but local dev |
| `SPAWNER_CONTAINER_MEMORY_MB` | `512` | hard ceiling per container; must be ≥ the largest `memory_limit_mb` Catalog ever publishes |

## Deploying

The Spawner's own container needs the Docker socket mounted
(`-v /var/run/docker.sock:/var/run/docker.sock`) to do its job. **No other
service should ever get that mount**, that's the entire point of splitting this
out from the Judge Worker.
