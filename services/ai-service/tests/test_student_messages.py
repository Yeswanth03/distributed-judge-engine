import pytest
from fastapi.testclient import TestClient

from app import api
from app.schemas import NOT_AVAILABLE, TAKING_LONGER, TUTOR_FAILED, TUTOR_WAITING, student_message
from tests.fakes import SUBMISSION, USER, FakeDatabase


@pytest.mark.parametrize("status, reason, expected", [
    ("ready", None, None),
    ("queued", None, None),                                        # normal: the page shows its spinner
    ("processing", None, None),
    ("queued", "waiting: ollama circuit is open; retry in 10s", TUTOR_WAITING),
    ("queued", "generating in the background (ReadTimeout)", TAKING_LONGER),
    ("unavailable", "HTTPStatusError: Client error '404 Not Found' for url 'http://host.docker.internal:11434/api/generate'", TUTOR_FAILED),
    ("unavailable", "ReadTimeout: timed out", TUTOR_FAILED),
    ("unavailable", "the hint contained hidden test data and was blocked", TUTOR_FAILED),
    ("unavailable", "submission dc7626b3 not found for this user", NOT_AVAILABLE),
    ("unavailable", "problem two-sum has no published test set", NOT_AVAILABLE),
])
def test_student_message(status, reason, expected):
    assert student_message(status, reason) == expected


def test_api_never_sends_the_internal_reason(monkeypatch):
    db = FakeDatabase()
    row = db.create_request(submission_id=SUBMISSION, user_id=USER, problem_id=None, verdict="wrong_answer",
                            rejudge_seq=0, trigger="failed_verdict", idempotency_key="k")
    db.mark_unavailable(row["request_id"], "ConnectError: [Errno 111] Connection refused to http://host.docker.internal:11434")
    monkeypatch.setattr(api, "feedback_database", db)
    body = TestClient(api.app).get(f"/feedback-requests/{row['request_id']}", headers={"X-User-Id": USER}).json()
    assert body["status"] == "unavailable" and body["message"] == TUTOR_FAILED
    assert "error_reason" not in body and "host.docker.internal" not in str(body)
