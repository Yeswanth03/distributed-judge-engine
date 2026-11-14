from datetime import datetime, timedelta, timezone
from uuid import UUID

import httpx
import pytest
from fastapi.testclient import TestClient

from app import api
from app.clients import ContextNotFound
from app.config import settings
from app.feedback import FeedbackProcessor
from app.models import FailedVerdictEvent
from app.resilience import CircuitBreaker, CircuitOpenError, CircuitState, backoff_delay
from tests.fakes import SUBMISSION, USER, FakeDatabase, FakeOllama, FakeServices
from tests.test_feedback import verdict_event


class FakeClock:
    def __init__(self):
        self.now = 1000.0

    def __call__(self):
        return self.now

    def advance(self, seconds):
        self.now += seconds


def server_error():
    request = httpx.Request("GET", "http://dep/x")
    return httpx.HTTPStatusError("boom", request=request, response=httpx.Response(503, request=request))


def not_found():
    request = httpx.Request("GET", "http://dep/x")
    return httpx.HTTPStatusError("missing", request=request, response=httpx.Response(404, request=request))


def raiser(exc):
    def fn():
        raise exc
    return fn


@pytest.fixture
def clock():
    return FakeClock()


@pytest.fixture
def breaker(clock):
    # Lab 3's shape: 5 failures in the last 10 calls opens; 10 s later, one trial.
    return CircuitBreaker("dep", failure_threshold=5, window_size=10, open_seconds=10, clock=clock)


def fail(breaker, times, exc_factory=server_error):
    for _ in range(times):
        with pytest.raises(Exception):
            breaker.call(raiser(exc_factory()))


# ---- the state machine ----

def test_stays_closed_below_threshold(breaker):
    fail(breaker, 4)
    assert breaker.state is CircuitState.CLOSED
    assert breaker.call(lambda: "ok") == "ok"


def test_opens_after_threshold_and_fails_fast(breaker):
    fail(breaker, 5)
    assert breaker.state is CircuitState.OPEN

    calls = []
    with pytest.raises(CircuitOpenError) as info:
        breaker.call(lambda: calls.append(1))
    assert calls == []  # the dependency was never touched
    assert info.value.dependency == "dep" and 0 < info.value.retry_after <= 10


def test_failures_slide_out_of_the_window(breaker):
    fail(breaker, 4)
    for _ in range(10):  # ten successes push the four failures out of the window
        breaker.call(lambda: None)
    fail(breaker, 4)
    assert breaker.state is CircuitState.CLOSED


@pytest.mark.parametrize("exc_factory", [not_found, lambda: ContextNotFound("no such submission"), lambda: KeyError("x")])
def test_caller_errors_do_not_count(breaker, exc_factory):
    fail(breaker, 10, exc_factory)
    assert breaker.state is CircuitState.CLOSED


@pytest.mark.parametrize("exc_factory", [lambda: httpx.ConnectError("refused"), lambda: httpx.ReadTimeout("slow"), server_error])
def test_outages_count(breaker, exc_factory):
    fail(breaker, 5, exc_factory)
    assert breaker.state is CircuitState.OPEN


def test_half_open_trial_success_closes(breaker, clock):
    fail(breaker, 5)
    clock.advance(10)
    assert breaker.state is CircuitState.HALF_OPEN
    assert breaker.call(lambda: "recovered") == "recovered"
    assert breaker.state is CircuitState.CLOSED
    fail(breaker, 4)  # old failures were cleared: four new ones don't reopen it
    assert breaker.state is CircuitState.CLOSED


def test_half_open_trial_failure_reopens_with_fresh_timer(breaker, clock):
    fail(breaker, 5)
    clock.advance(10)
    fail(breaker, 1)
    assert breaker.state is CircuitState.OPEN
    clock.advance(9)
    assert breaker.state is CircuitState.OPEN  # timer restarted at the failed trial
    clock.advance(1)
    assert breaker.state is CircuitState.HALF_OPEN


def test_only_one_trial_at_a_time(breaker, clock):
    fail(breaker, 5)
    clock.advance(10)

    def trial():
        with pytest.raises(CircuitOpenError):  # a second caller during the trial fails fast
            breaker.call(lambda: None)
        return "trial done"

    assert breaker.call(trial) == "trial done"
    assert breaker.state is CircuitState.CLOSED


def test_listener_sees_every_transition(breaker, clock):
    seen = []
    breaker.on_state_change(lambda name, old, new: seen.append((old.value, new.value)))
    fail(breaker, 5)
    clock.advance(10)
    breaker.call(lambda: None)
    assert seen == [("closed", "open"), ("open", "half_open"), ("half_open", "closed")]


def test_snapshot_reports_state_and_retry_after(breaker, clock):
    fail(breaker, 5)
    clock.advance(4)
    snap = breaker.snapshot()
    assert snap["state"] == "open" and snap["retry_after_seconds"] == 6.0 and snap["recent_failures"] == 5


def test_backoff_doubles_with_jitter_and_caps():
    for attempt, ceiling in [(1, 1), (2, 2), (3, 4), (4, 8), (5, 8), (9, 8)]:
        delays = [backoff_delay(attempt, base=1, cap=8) for _ in range(200)]
        assert all(ceiling / 2 <= d <= ceiling for d in delays)
        assert len({round(d, 6) for d in delays}) > 1  # jittered, not a fixed wait


# ---- the breaker inside the feedback flow ----

@pytest.fixture
def services():
    return FakeServices()


@pytest.fixture
def processor(services, clock):
    submissions, catalog = services.clients(clock=clock)
    return FeedbackProcessor(database=FakeDatabase(), submissions=submissions, catalog=catalog,
                             ollama=FakeOllama(clock=clock), retry_delay_seconds=0)


def queue_request(processor, n):
    sub = f"{n:08d}-0000-4000-8000-000000000000"
    return processor.process_failed_verdict(FailedVerdictEvent.model_validate(verdict_event(submission_id=sub)))


def test_ollama_outage_opens_circuit_and_defers_instead_of_failing(processor, services):
    services.submission["submission_id"] = SUBMISSION
    processor.ollama.down = True
    first, second, third = (queue_request(processor, n) for n in (1, 2, 3))
    # The fake Submission Service only knows SUBMISSION; point every request at it.
    for row in (first, second, third):
        row["submission_id"] = UUID(SUBMISSION)

    processor.generate(first["request_id"])   # 3 failed attempts -> unavailable
    processor.generate(second["request_id"])  # 2 more failures -> circuit opens -> deferred
    calls_so_far = processor.ollama.calls
    processor.generate(third["request_id"])   # fails fast: Ollama not called at all

    rows = processor.database.rows
    assert rows[first["request_id"]]["status"] == "unavailable"
    assert processor.ollama.breaker.state is CircuitState.OPEN
    for req in (second, third):
        assert rows[req["request_id"]]["status"] == "queued"
        assert "ollama circuit is open" in rows[req["request_id"]]["error_reason"]
    assert calls_so_far == 5 and processor.ollama.calls == 5


def test_deferred_request_completes_after_recovery(processor, clock):
    processor.ollama.down = True
    for _ in range(5):
        with pytest.raises(httpx.ConnectError):
            processor.ollama.generate_feedback(None)
    row = processor.process_failed_verdict(FailedVerdictEvent.model_validate(verdict_event()))
    processor.generate(row["request_id"])
    assert processor.database.rows[row["request_id"]]["status"] == "queued"

    processor.ollama.down = False
    clock.advance(settings.breaker_open_seconds)  # half-open: the next call is the trial
    processor.generate(row["request_id"])          # what the sweep does on its next tick

    saved = processor.database.rows[row["request_id"]]
    assert saved["status"] == "ready" and saved["error_reason"] is None
    assert processor.ollama.breaker.state is CircuitState.CLOSED


def test_deferral_gives_up_after_max_defer_seconds(processor):
    processor.ollama.down = True
    for _ in range(5):
        with pytest.raises(httpx.ConnectError):
            processor.ollama.generate_feedback(None)
    row = processor.process_failed_verdict(FailedVerdictEvent.model_validate(verdict_event()))
    row["created_at"] = datetime.now(timezone.utc) - timedelta(seconds=settings.max_defer_seconds + 1)
    processor.generate(row["request_id"])
    saved = processor.database.rows[row["request_id"]]
    assert saved["status"] == "unavailable" and "ollama unavailable for over" in saved["error_reason"]


def test_breakers_are_isolated_per_dependency(processor, services):
    services.problem_status = 503  # Catalog is failing; Submission is fine
    for n in range(2):
        row = processor.process_failed_verdict(FailedVerdictEvent.model_validate(verdict_event(rejudge_seq=n)))
        processor.generate(row["request_id"])
    assert processor.catalog.breaker.state is CircuitState.OPEN
    assert processor.submissions.breaker.state is CircuitState.CLOSED
    assert processor.ollama.breaker.state is CircuitState.CLOSED


def test_someone_elses_submissions_never_open_the_circuit(processor):
    for _ in range(20):
        with pytest.raises(ContextNotFound):
            processor.request_on_demand(UUID(SUBMISSION), "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
    assert processor.submissions.breaker.state is CircuitState.CLOSED


# ---- the API fails fast while a circuit is open ----

@pytest.fixture
def client(processor, monkeypatch):
    monkeypatch.setattr(api, "feedback_processor", processor)
    monkeypatch.setattr(api, "feedback_database", processor.database)
    monkeypatch.setattr(api, "breakers", {"ollama": processor.ollama.breaker,
                                          "submission": processor.submissions.breaker,
                                          "catalog": processor.catalog.breaker})
    return TestClient(api.app)


def test_api_returns_503_with_retry_after_when_submission_circuit_open(client, services):
    headers = {"X-User-Id": USER}
    services.submission_status = 503
    for _ in range(5):  # five 503s from the Submission Service open its circuit
        assert client.post(f"/submissions/{SUBMISSION}/feedback", headers=headers).status_code == 502

    resp = client.post(f"/submissions/{SUBMISSION}/feedback", headers=headers)
    assert resp.status_code == 503
    body = resp.json()
    assert body["status"] == "circuit_open" and body["dependency"] == "submission"
    assert 1 <= body["retry_after"] <= 10 and resp.headers["Retry-After"] == str(body["retry_after"])


def test_health_reports_circuit_states(client, processor):
    healthy = client.get("/health").json()
    assert healthy["status"] == "ok" and healthy["circuits"]["ollama"]["state"] == "closed"

    processor.ollama.down = True
    for _ in range(5):
        with pytest.raises(httpx.ConnectError):
            processor.ollama.generate_feedback(None)
    degraded = client.get("/health")
    assert degraded.status_code == 200
    assert degraded.json()["status"] == "degraded" and degraded.json()["circuits"]["ollama"]["state"] == "open"
