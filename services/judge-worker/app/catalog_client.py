import httpx

from .config import settings


class CatalogClient:
    """Talks to Catalog's internal, judge-only API. Never routed through the
    gateway; authenticated with the shared X-Service-Token, not a user token."""

    def __init__(self):
        self._client = httpx.Client(
            base_url=settings.catalog_base_url,
            timeout=5.0,
            headers={"X-Service-Token": settings.internal_service_token},
        )

    def get_testset(self, problem_id: str, version: int | None = None) -> dict:
        params = {"version": version} if version is not None else {}
        resp = self._client.get(f"/internal/problems/{problem_id}/testset", params=params)
        resp.raise_for_status()
        return resp.json()
