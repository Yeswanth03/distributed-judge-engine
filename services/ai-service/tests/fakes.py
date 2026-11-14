"""Stand-ins for the Submission Service, Catalog and the feedback database.

The fake services implement the same paths, headers and JSON shapes as the real
ones (services/submission-service handler/get.go, Catalog's ProblemController and
InternalTestsetController), so the real HTTP clients are exercised, not mocked away.
"""

from datetime import datetime, timezone
from uuid import uuid4

import httpx

from app.clients import CatalogClient, SubmissionClient
from app.resilience import new_breaker

USER = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
SUBMISSION = "dc7626b3-f4e7-4194-a625-300911712913"
PROBLEM_UUID = "11111111-1111-1111-1111-111111111111"
TOKEN = "local-dev-token"

HIDDEN_INPUT = "SECRET-HIDDEN-INPUT-999"
HIDDEN_OUTPUT = "SECRET-HIDDEN-OUTPUT-999"


def submission_body(**overrides) -> dict:
    body = {
        "submission_id": SUBMISSION,
        "user_id": USER,
        "problem_id": PROBLEM_UUID,
        "language": "python",
        "code": "a, b = map(int, input().split())\nprint(a - b)",
        "status": "COMPLETED",
        "verdict": "WRONG_ANSWER",
        "execution_time_ms": 30,
        "memory_kb": 9000,
        "test_cases_passed": 1,
        "total_test_cases": 3,
        "error_message": None,
        "created_at": "2026-11-03T10:00:00Z",
        "updated_at": "2026-11-03T10:00:02Z",
    }
    body.update(overrides)
    return body


PROBLEM = {
    "id": PROBLEM_UUID,
    "slug": "sum-two",
    "title": "Sum Two",
    "statement_md": "Read two integers and print their sum.",
    "difficulty": "easy",
    "points": 100,
    "time_limit_ms": 1000,
    "memory_limit_mb": 256,
    "samples": [{"input": "2 3\n", "expected_output": "5"}],
}

TESTSET = {
    "problem_id": PROBLEM_UUID,
    "version": 1,
    "content_hash": "abc",
    "time_limit_ms": 1000,
    "memory_limit_mb": 256,
    "cases": [
        {"ordinal": 1, "input": "1 1\n", "expected_output": "2", "is_sample": False},
        {"ordinal": 2, "input": "2 3\n", "expected_output": "5", "is_sample": True},
        {"ordinal": 3, "input": HIDDEN_INPUT, "expected_output": HIDDEN_OUTPUT, "is_sample": False},
    ],
}


class FakeServices:
    """Records requests; tweak the attributes to simulate failures."""

    def __init__(self):
        self.requests: list[httpx.Request] = []
        self.submission = submission_body()
        self.submission_status = 200
        self.problem_status = 200
        self.testset_versions = {1: TESTSET}

    def handle(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        path = request.url.path
        if path == f"/api/v1/submissions/{SUBMISSION}":
            if request.headers.get("X-User-Id") != self.submission["user_id"]:
                return httpx.Response(404, json={"error": "not_found", "message": "submission not found"})
            if self.submission_status != 200:
                return httpx.Response(self.submission_status, json={"error": "x", "message": "x"})
            body = dict(self.submission)
            if request.url.params.get("include") != "code":
                body.pop("code")
            return httpx.Response(200, json=body)
        if path.startswith("/api/v1/submissions/"):
            return httpx.Response(404, json={"error": "not_found", "message": "submission not found"})
        if path == "/problems":
            # Public list, paged like Catalog's: id and slug of each published problem.
            other = {"id": "22222222-2222-2222-2222-222222222222", "slug": "aaa-first",
                     "title": "First", "difficulty": "easy", "points": 10}
            mine = {k: PROBLEM[k] for k in ("id", "slug", "title", "difficulty", "points")}
            if request.url.params.get("cursor") == "aaa-first":
                return httpx.Response(200, json={"items": [mine], "next_cursor": None})
            return httpx.Response(200, json={"items": [other], "next_cursor": "aaa-first"})
        if path == "/problems/sum-two":
            if self.problem_status != 200:
                return httpx.Response(self.problem_status, json={"error": "x", "message": "x"})
            return httpx.Response(200, json=PROBLEM)
        if path == f"/internal/problems/{PROBLEM_UUID}/testset":
            if request.headers.get("X-Service-Token") != TOKEN:
                return httpx.Response(401, json={"error": "unauthorized", "message": "valid service token required"})
            version = request.url.params.get("version")
            testset = self.testset_versions.get(int(version)) if version else self.testset_versions[max(self.testset_versions)]
            if testset is None:
                return httpx.Response(404, json={"error": "not_found", "message": "not published"})
            return httpx.Response(200, json=testset)
        return httpx.Response(404, json={"error": "not_found", "message": "no such route"})

    def clients(self, clock=None) -> tuple[SubmissionClient, CatalogClient]:
        """Real clients over a fake transport, each with its own fresh circuit breaker
        (so one test's failures never leak into another through the shared breakers)."""
        transport = httpx.MockTransport(self.handle)
        extra = {"clock": clock} if clock else {}
        return (
            SubmissionClient(httpx.Client(base_url="http://submission", transport=transport),
                             breaker=new_breaker("submission", **extra)),
            CatalogClient(httpx.Client(base_url="http://catalog", transport=transport),
                          breaker=new_breaker("catalog", **extra)),
        )


class FakeDatabase:
    """In-memory version of FeedbackDatabase with the same method contracts."""

    def __init__(self):
        self.rows: dict = {}

    def _now(self):
        return datetime.now(timezone.utc)

    def _new_row(self, **fields) -> dict:
        now = self._now()
        row = {
            "request_id": uuid4(), "status": "queued", "feedback": None, "error_reason": None,
            "model_name": "llama3.2", "prompt_version": "2", "input_tokens": None,
            "output_tokens": None, "created_at": now, "updated_at": now, **fields,
        }
        self.rows[row["request_id"]] = row
        return row

    def create_request(self, *, submission_id, user_id, problem_id, verdict, rejudge_seq, trigger,
                       idempotency_key, judge_details=None):
        for row in self.rows.values():
            if row["user_id"] == user_id and row["idempotency_key"] == idempotency_key:
                return row
        return self._new_row(submission_id=submission_id, user_id=user_id, problem_id=problem_id,
                             verdict=verdict, rejudge_seq=rejudge_seq, request_source=trigger,
                             idempotency_key=idempotency_key, judge_details=judge_details)

    def create_or_reuse_user_request(self, *, submission_id, user_id, problem_id, verdict):
        active = [r for r in self.rows.values()
                  if r["user_id"] == user_id and str(r["submission_id"]) == str(submission_id)
                  and r["status"] in ("queued", "processing", "ready")]
        if active:
            return max(active, key=lambda r: r["created_at"]), False
        return self._new_row(submission_id=submission_id, user_id=user_id, problem_id=problem_id,
                             verdict=verdict, rejudge_seq=0, request_source="user_request",
                             idempotency_key=f"user:{submission_id}:{uuid4()}", judge_details=None), True

    def claim(self, request_id):
        row = self.rows.get(request_id)
        if row is None or row["status"] != "queued":
            return None
        row["status"] = "processing"
        return row

    def requeue_for_retry(self, request_id, reason=None):
        if self.rows[request_id]["status"] == "processing":
            self.rows[request_id].update(status="queued", error_reason=reason)

    def set_problem_id(self, request_id, problem_id):
        self.rows[request_id]["problem_id"] = problem_id

    def save_feedback(self, request_id, feedback, input_tokens, output_tokens):
        self.rows[request_id].update(status="ready", feedback=feedback, input_tokens=input_tokens,
                                     output_tokens=output_tokens, error_reason=None)
        return self.rows[request_id]

    def mark_unavailable(self, request_id, reason):
        self.rows[request_id].update(status="unavailable", error_reason=reason)

    def pending_request_ids(self, stale_after_seconds, limit=50):
        return [r["request_id"] for r in self.rows.values() if r["status"] == "queued"][:limit]

    def get_request(self, request_id, user_id):
        row = self.rows.get(request_id)
        return row if row and row["user_id"] == user_id else None

    def get_latest_for_submission(self, submission_id, user_id):
        rows = [r for r in self.rows.values()
                if str(r["submission_id"]) == str(submission_id) and r["user_id"] == user_id]
        return max(rows, key=lambda r: r["created_at"]) if rows else None


class FakeOllama:
    """Goes through a real circuit breaker like OllamaClient. `failures` fails the
    next N calls; `down = True` fails every call until set back to False."""

    def __init__(self, failures: int = 0, clock=None):
        self.calls = 0           # every call that actually reached "Ollama"
        self.prompts: list = []  # prompts of the calls that succeeded
        self.failures = failures
        self.down = False
        self.breaker = new_breaker("ollama", **({"clock": clock} if clock else {}))

    def generate_feedback(self, context):
        return self.breaker.call(lambda: self._generate(context))

    def _generate(self, context):
        from app.ollama_client import GeneratedFeedback, build_prompt

        self.calls += 1
        if self.down or self.failures:
            self.failures = max(0, self.failures - 1)
            raise httpx.ConnectError("ollama down")
        self.prompts.append(build_prompt(context))
        return GeneratedFeedback(text="Check the operator: you subtract instead of add.",
                                 input_tokens=120, output_tokens=12)
