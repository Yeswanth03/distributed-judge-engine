import httpx

from .config import settings


class SpawnerClient:
    """The Judge Worker's only way to run untrusted code: it never touches Docker
    itself. A 503 from the Spawner means the pool is exhausted, which is
    backpressure, not a submission failure, so callers should let that propagate
    and leave the job unacked rather than publishing a verdict for it."""

    def __init__(self):
        self._client = httpx.Client(base_url=settings.spawner_base_url, timeout=30.0)

    def execute(
        self,
        submission_id: str,
        language: str,
        source_code: str,
        stdin: str,
        time_limit_ms: int,
        memory_limit_mb: int,
    ) -> dict:
        resp = self._client.post(
            "/execute",
            json={
                "submission_id": submission_id,
                "language": language,
                "source_code": source_code,
                "stdin": stdin,
                "time_limit_ms": time_limit_ms,
                "memory_limit_mb": memory_limit_mb,
            },
        )
        resp.raise_for_status()
        return resp.json()
