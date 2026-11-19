"""POST /submissions/{id}/feedback/direct: hints generated inside the request, accepted submissions only."""
from uuid import UUID

import httpx
import pytest
from fastapi.testclient import TestClient
from prometheus_client import REGISTRY

from app import api
from app.config import settings
from app.feedback import FeedbackProcessor, NotAccepted
from tests.fakes import SUBMISSION, USER, FakeDatabase, FakeOllama, FakeServices

DIRECT = f"/submissions/{SUBMISSION}/feedback/direct"
QUEUED = f"/submissions/{SUBMISSION}/feedback"
HEADERS = {"X-User-Id": USER}


@pytest.fixture
def services():
    s = FakeServices()
    s.submission.update(verdict="ACCEPTED", test_cases_passed=3, total_test_cases=3)
    return s


@pytest.fixture
def processor(services):
    submissions, catalog = services.clients()
    return FeedbackProcessor(database=FakeDatabase(), submissions=submissions, catalog=catalog,
                             ollama=FakeOllama(), retry_delay_seconds=0)


@pytest.fixture
def client(processor, monkeypatch):
    monkeypatch.setattr(api, "feedback_processor", processor)
    monkeypatch.setattr(api, "feedback_database", processor.database)
    return TestClient(api.app)


def direct_count(outcome):
    return REGISTRY.get_sample_value("ai_direct_feedback_total", {"outcome": outcome}) or 0.0


# ---- accepted: hints come back in the same request ----

def test_accepted_submission_gets_hints_in_the_response(client, processor):
    before = direct_count("ready")
    resp = client.post(DIRECT, headers=HEADERS)
    assert resp.status_code == 200, resp.text
    body = resp.json()
    assert body["status"] == "ready" and body["verdict"] == "accepted" and body["feedback"]
    assert body["request_source"] == "user_request"
    assert processor.ollama.calls == 1                                # generated now, not queued
    assert processor.ollama.last_timeout == settings.direct_timeout_seconds  # bounded by the gateway limit
    assert "Verdict: accepted" in processor.ollama.prompts[0]
    assert direct_count("ready") == before + 1
    # The saved hints are what the existing read endpoint returns.
    assert client.get(QUEUED, headers=HEADERS).json()["request_id"] == body["request_id"]


def test_clicking_again_returns_the_saved_hints(client, processor):
    first = client.post(DIRECT, headers=HEADERS).json()
    second = client.post(DIRECT, headers=HEADERS)
    assert second.status_code == 200 and second.json()["request_id"] == first["request_id"]
    assert processor.ollama.calls == 1


def test_direct_timeout_fits_inside_the_gateway_timeout():
    assert settings.direct_timeout_seconds < 15  # envoy: /api/v1/ai/* times out at 15 s


# ---- slow or failing model: falls back to the background queue, never an error ----

def test_model_failure_falls_back_to_the_queue(client, processor):
    before = direct_count("fallback")
    processor.ollama.down = True
    resp = client.post(DIRECT, headers=HEADERS)
    assert resp.status_code == 202
    body = resp.json()
    assert body["status"] == "queued" and "background" in body["error_reason"]
    assert direct_count("fallback") == before + 1

    processor.ollama.down = False
    processor.generate(UUID(body["request_id"]))  # what the background worker / sweep does
    assert client.get(f"/feedback-requests/{body['request_id']}", headers=HEADERS).json()["status"] == "ready"


def test_model_timeout_falls_back_to_the_queue(client, processor, monkeypatch):
    def slow(context, timeout=None):
        raise httpx.ReadTimeout("model took too long")

    monkeypatch.setattr(processor.ollama, "generate_feedback", slow)
    resp = client.post(DIRECT, headers=HEADERS)
    assert resp.status_code == 202 and resp.json()["status"] == "queued"


def test_open_ollama_circuit_falls_back_without_calling_the_model(client, processor):
    processor.ollama.down = True
    for _ in range(5):
        with pytest.raises(httpx.ConnectError):
            processor.ollama.generate_feedback(None)
    calls = processor.ollama.calls
    resp = client.post(DIRECT, headers=HEADERS)
    assert resp.status_code == 202 and processor.ollama.calls == calls


# ---- only for accepted; the failed-submission path is unchanged ----

@pytest.mark.parametrize("verdict", ["WRONG_ANSWER", "RUNTIME_ERROR", "TIME_LIMIT_EXCEEDED", None])
def test_direct_refuses_anything_not_accepted(client, services, processor, verdict):
    services.submission.update(verdict=verdict, status="COMPLETED" if verdict else "RUNNING")
    resp = client.post(DIRECT, headers=HEADERS)
    assert resp.status_code == 409
    assert processor.ollama.calls == 0 and processor.database.rows == {}


def test_wrong_answer_still_uses_the_queued_endpoint(client, services, processor):
    services.submission.update(verdict="WRONG_ANSWER", test_cases_passed=1)
    resp = client.post(QUEUED, headers=HEADERS)
    assert resp.status_code == 202 and resp.json()["status"] == "queued"  # unchanged behaviour
    assert processor.ollama.calls == 0                                   # nothing generated in the request


def test_processor_refuses_failed_submissions(processor, services):
    services.submission.update(verdict="WRONG_ANSWER")
    with pytest.raises(NotAccepted):
        processor.request_direct(UUID(SUBMISSION), USER)


# ---- errors ----

def test_someone_elses_submission_is_404(client):
    assert client.post(DIRECT, headers={"X-User-Id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}).status_code == 404


def test_submission_service_down_is_502_then_503(client, services):
    services.submission_status = 503
    for _ in range(5):
        assert client.post(DIRECT, headers=HEADERS).status_code == 502
    resp = client.post(DIRECT, headers=HEADERS)
    assert resp.status_code == 503 and resp.json()["status"] == "circuit_open"
