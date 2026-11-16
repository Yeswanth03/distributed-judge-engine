import json
from types import SimpleNamespace
from uuid import UUID

import httpx
import pytest
from fastapi.testclient import TestClient
from prometheus_client import REGISTRY

from app import api, metrics
from app.broker import _on_message
from app.config import settings
from app.feedback import FeedbackProcessor
from app.models import FailedVerdictEvent
from app.resilience import CircuitOpenError, new_breaker
from tests.fakes import SUBMISSION, USER, FakeDatabase, FakeOllama, FakeServices
from tests.test_feedback import FakeChannel, verdict_event


def value(name, **labels):
    """Current value of a metric sample (0 if it has never been recorded)."""
    return REGISTRY.get_sample_value(name, labels) or 0.0


@pytest.fixture
def services():
    return FakeServices()


@pytest.fixture
def processor(services):
    submissions, catalog = services.clients()
    return FeedbackProcessor(database=FakeDatabase(), submissions=submissions, catalog=catalog,
                             ollama=FakeOllama(), retry_delay_seconds=0)


def queue(processor, **overrides):
    return processor.process_failed_verdict(FailedVerdictEvent.model_validate(verdict_event(**overrides)))


def test_ready_feedback_records_outcome_tokens_and_timings(processor):
    model = settings.ollama_model
    before = {
        "events": value("ai_verdict_events_total", verdict="wrong_answer"),
        "ready": value("ai_feedback_outcomes_total", source="failed_verdict", outcome="ready"),
        "in": value("ai_llm_tokens_total", model=model, direction="input"),
        "out": value("ai_llm_tokens_total", model=model, direction="output"),
        "gen": value("ai_llm_generation_duration_seconds_count", model=model),
        "wait": value("ai_feedback_time_to_ready_seconds_count", source="failed_verdict"),
    }
    row = queue(processor)
    processor.generate(row["request_id"])

    assert value("ai_verdict_events_total", verdict="wrong_answer") == before["events"] + 1
    assert value("ai_feedback_outcomes_total", source="failed_verdict", outcome="ready") == before["ready"] + 1
    assert value("ai_llm_tokens_total", model=model, direction="input") == before["in"] + 120  # FakeOllama's counts
    assert value("ai_llm_tokens_total", model=model, direction="output") == before["out"] + 12
    assert value("ai_llm_generation_duration_seconds_count", model=model) == before["gen"] + 1
    assert value("ai_feedback_time_to_ready_seconds_count", source="failed_verdict") == before["wait"] + 1
    assert value("ai_generations_in_flight") == 0  # back to idle afterwards


def test_unavailable_and_retries_are_counted(processor, services):
    services.submission_status = 503
    unavailable = value("ai_feedback_outcomes_total", source="failed_verdict", outcome="unavailable")
    retries = value("ai_generation_retries_total", error="HTTPStatusError")
    row = queue(processor)
    processor.generate(row["request_id"])
    assert value("ai_feedback_outcomes_total", source="failed_verdict", outcome="unavailable") == unavailable + 1
    assert value("ai_generation_retries_total", error="HTTPStatusError") == retries + settings.generation_attempts - 1


def test_deferred_is_counted(processor):
    processor.ollama.down = True
    for _ in range(5):
        with pytest.raises(httpx.ConnectError):
            processor.ollama.generate_feedback(None)
    deferred = value("ai_feedback_outcomes_total", source="failed_verdict", outcome="deferred")
    row = queue(processor)
    processor.generate(row["request_id"])
    assert value("ai_feedback_outcomes_total", source="failed_verdict", outcome="deferred") == deferred + 1


def test_breaker_state_transitions_and_calls_are_tracked():
    breaker = new_breaker("metrics-test-dep", open_seconds=1000)
    metrics.track_breaker(breaker)
    assert value("ai_circuit_breaker_state", dependency="metrics-test-dep") == 0

    def boom():
        raise httpx.ConnectError("down")

    for _ in range(5):
        with pytest.raises(httpx.ConnectError):
            breaker.call(boom)
    with pytest.raises(CircuitOpenError):
        breaker.call(lambda: None)

    assert value("ai_circuit_breaker_state", dependency="metrics-test-dep") == 2
    assert value("ai_circuit_breaker_transitions_total", dependency="metrics-test-dep", to_state="open") == 1
    assert value("ai_dependency_calls_total", dependency="metrics-test-dep", outcome="failure") == 5
    assert value("ai_dependency_calls_total", dependency="metrics-test-dep", outcome="rejected") == 1
    assert value("ai_dependency_call_duration_seconds_count", dependency="metrics-test-dep") == 5  # rejected calls have no duration


def test_caller_errors_are_counted_separately_from_failures():
    breaker = new_breaker("metrics-test-404")
    metrics.track_breaker(breaker)
    request = httpx.Request("GET", "http://dep/x")

    def not_found():
        raise httpx.HTTPStatusError("404", request=request, response=httpx.Response(404, request=request))

    for _ in range(3):
        with pytest.raises(httpx.HTTPStatusError):
            breaker.call(not_found)
    assert value("ai_dependency_calls_total", dependency="metrics-test-404", outcome="error") == 3
    assert value("ai_dependency_calls_total", dependency="metrics-test-404", outcome="failure") == 0


def test_dead_lettered_events_are_counted(processor):
    invalid = value("ai_verdict_events_rejected_total", reason="invalid")
    mismatch = value("ai_verdict_events_rejected_total", reason="routing_mismatch")
    callback = _on_message(processor.process_failed_verdict)
    callback(FakeChannel(), SimpleNamespace(delivery_tag=1, routing_key="verdict.wrong_answer"), None, b"not json")
    callback(FakeChannel(), SimpleNamespace(delivery_tag=2, routing_key="verdict.runtime_error"), None,
             json.dumps(verdict_event()).encode())
    assert value("ai_verdict_events_rejected_total", reason="invalid") == invalid + 1
    assert value("ai_verdict_events_rejected_total", reason="routing_mismatch") == mismatch + 1


# ---- HTTP metrics and the /metrics endpoint ----

@pytest.fixture
def client(processor, monkeypatch):
    monkeypatch.setattr(api, "feedback_processor", processor)
    monkeypatch.setattr(api, "feedback_database", processor.database)
    return TestClient(api.app)


def test_http_requests_are_counted_per_route_template(client):
    route = "/submissions/{submission_id}/feedback"
    before = value("ai_http_requests_total", method="POST", route=route, status="202")
    latency_before = value("ai_http_request_duration_seconds_count", method="POST", route=route)
    assert client.post(f"/submissions/{SUBMISSION}/feedback", headers={"X-User-Id": USER}).status_code == 202
    assert value("ai_http_requests_total", method="POST", route=route, status="202") == before + 1
    assert value("ai_http_request_duration_seconds_count", method="POST", route=route) == latency_before + 1


def test_metrics_endpoint_exposes_everything_without_ids(client):
    client.post(f"/submissions/{SUBMISSION}/feedback", headers={"X-User-Id": USER})
    resp = client.get("/metrics")
    assert resp.status_code == 200 and resp.headers["content-type"].startswith("text/plain")
    body = resp.text
    for name in ["ai_http_requests_total", "ai_http_request_duration_seconds_bucket", "ai_circuit_breaker_state",
                 "ai_llm_tokens_total", "ai_feedback_outcomes_total", "ai_generations_in_flight",
                 "ai_dependency_calls_total"]:
        assert name in body, name
    assert SUBMISSION not in body and USER not in body  # IDs never become labels
    assert 'route="/metrics"' not in body              # scrapes don't count as traffic


def test_production_breakers_are_reported(client):
    body = client.get("/metrics").text
    for dependency in ("ollama", "submission", "catalog"):
        assert f'ai_circuit_breaker_state{{dependency="{dependency}"}}' in body


def test_sweep_reports_waiting_requests(processor):
    queue(processor)
    for status, count in processor.database.status_counts().items():
        metrics.FEEDBACK_REQUESTS.labels(status).set(count)
    assert value("ai_feedback_requests", status="queued") == 1
