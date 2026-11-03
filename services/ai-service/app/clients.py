"""HTTP clients for the services that hold a submission's context.

The flow mirrors what the Judge Worker already does:
  Submission Service  GET /api/v1/submissions/{id}?include=code  -> code, language, problem slug, verdict
  Catalog             GET /problems/{slug}                       -> problem UUID, statement, limits
  Catalog             GET /internal/problems/{uuid}/testset      -> test cases (X-Service-Token)
"""

from dataclasses import dataclass, field

import httpx

from .config import settings
from .models import normalize_verdict


class ContextNotFound(Exception):
    """The submission or problem does not exist (or is not visible). Retrying will not help."""


@dataclass(frozen=True)
class SubmissionDetails:
    submission_id: str
    user_id: str
    problem_slug: str
    language: str
    code: str
    status: str
    verdict: str | None
    test_cases_passed: int | None
    total_test_cases: int | None
    error_message: str | None


@dataclass(frozen=True)
class TestCase:
    ordinal: int
    input: str
    expected_output: str
    is_sample: bool


@dataclass(frozen=True)
class ProblemDetails:
    problem_id: str
    slug: str
    title: str
    statement: str
    time_limit_ms: int
    memory_limit_mb: int
    testset_version: int | None = None
    cases: list[TestCase] = field(default_factory=list)

    @property
    def sample_cases(self) -> list[TestCase]:
        return [case for case in self.cases if case.is_sample]

    def case(self, ordinal: int) -> TestCase | None:
        return next((case for case in self.cases if case.ordinal == ordinal), None)


class SubmissionClient:
    """Reads a submission as its owner: the Submission Service only shows a
    submission to the user in X-User-Id (or an admin), so the AI service passes
    the user from the verdict event or the authenticated API caller."""

    def __init__(self, client: httpx.Client | None = None) -> None:
        self._client = client or httpx.Client(
            base_url=settings.submission_base_url,
            timeout=settings.http_timeout_seconds,
            headers={"X-Service-Token": settings.internal_service_token},
        )

    def get_submission(self, submission_id: str, user_id: str) -> SubmissionDetails:
        resp = self._client.get(
            f"/api/v1/submissions/{submission_id}",
            params={"include": "code"},
            headers={"X-User-Id": user_id},
        )
        if resp.status_code in (400, 401, 404):
            raise ContextNotFound(f"submission {submission_id} not found for this user")
        resp.raise_for_status()
        body = resp.json()
        code = body.get("code")
        if not code:
            raise ContextNotFound(f"submission {submission_id} has no source code")
        verdict = body.get("verdict")
        return SubmissionDetails(
            submission_id=str(body["submission_id"]),
            user_id=str(body["user_id"]),
            problem_slug=body["problem_id"],
            language=body["language"],
            code=code,
            status=body.get("status", ""),
            verdict=normalize_verdict(verdict) if verdict else None,
            test_cases_passed=body.get("test_cases_passed"),
            total_test_cases=body.get("total_test_cases"),
            error_message=body.get("error_message"),
        )


class CatalogClient:
    def __init__(self, client: httpx.Client | None = None) -> None:
        self._client = client or httpx.Client(
            base_url=settings.catalog_base_url,
            timeout=settings.http_timeout_seconds,
        )

    def get_problem(self, slug: str, testset_version: int | None = None) -> ProblemDetails:
        """Problem statement from the public read, then the test cases from the
        judge-only endpoint. testset_version pins the exact version that was judged."""
        resp = self._client.get(f"/problems/{slug}")
        if resp.status_code in (400, 404):
            raise ContextNotFound(f"problem {slug} not found in Catalog")
        resp.raise_for_status()
        problem = resp.json()

        params = {"version": testset_version} if testset_version is not None else {}
        resp = self._client.get(
            f"/internal/problems/{problem['id']}/testset",
            params=params,
            headers={"X-Service-Token": settings.internal_service_token},
        )
        if resp.status_code == 404 and testset_version is not None:
            # Fall back to the current version rather than giving no feedback.
            resp = self._client.get(
                f"/internal/problems/{problem['id']}/testset",
                headers={"X-Service-Token": settings.internal_service_token},
            )
        if resp.status_code == 404:
            raise ContextNotFound(f"problem {slug} has no published test set")
        resp.raise_for_status()
        testset = resp.json()

        return ProblemDetails(
            problem_id=str(problem["id"]),
            slug=problem["slug"],
            title=problem["title"],
            statement=problem["statement_md"],
            time_limit_ms=testset.get("time_limit_ms", problem["time_limit_ms"]),
            memory_limit_mb=testset.get("memory_limit_mb", problem["memory_limit_mb"]),
            testset_version=testset.get("version"),
            cases=[
                TestCase(
                    ordinal=case["ordinal"],
                    input=case["input"],
                    expected_output=case["expected_output"],
                    is_sample=bool(case.get("is_sample")),
                )
                for case in testset.get("cases", [])
            ],
        )
