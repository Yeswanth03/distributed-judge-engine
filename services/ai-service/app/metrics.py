"""Prometheus metrics for the AI service, exposed at GET /metrics.

They cover the four golden signals plus spend (module text Ch. 1: for an LLM
dependency, "spend is load", so token use sits on the same dashboard):

    traffic     ai_http_requests_total, ai_verdict_events_total
    errors      ai_http_requests_total{status=5xx}, ai_feedback_outcomes_total{outcome="unavailable"},
                ai_dependency_calls_total{outcome="failure"}, ai_verdict_events_rejected_total
    latency     ai_http_request_duration_seconds, ai_llm_generation_duration_seconds,
                ai_feedback_time_to_ready_seconds, ai_dependency_call_duration_seconds
    saturation  ai_generations_in_flight, ai_feedback_requests{status}
    spend       ai_llm_tokens_total{direction}
    resilience  ai_circuit_breaker_state, ai_circuit_breaker_transitions_total, ai_generation_retries_total

Labels only take a small, fixed set of values. User and submission IDs are never
labels: each distinct value would create a new time series.
"""

from prometheus_client import CONTENT_TYPE_LATEST, Counter, Gauge, Histogram, generate_latest

from .resilience import CircuitBreaker, CircuitState

# ---- traffic, errors and latency of the HTTP API ----

HTTP_REQUESTS = Counter(
    "ai_http_requests_total", "HTTP requests handled", ["method", "route", "status"]
)
HTTP_LATENCY = Histogram(
    "ai_http_request_duration_seconds", "HTTP request duration", ["method", "route"]
)

# ---- verdict events from RabbitMQ ----

VERDICT_EVENTS = Counter(
    "ai_verdict_events_total", "Failed-verdict events accepted from RabbitMQ", ["verdict"]
)
VERDICT_REJECTED = Counter(
    "ai_verdict_events_rejected_total", "Verdict events sent to the dead-letter queue", ["reason"]
)

# ---- feedback requests ----

FEEDBACK_REQUESTS_CREATED = Counter(
    "ai_feedback_requests_created_total", "Feedback requests created", ["source"]
)
FEEDBACK_OUTCOMES = Counter(
    "ai_feedback_outcomes_total", "How generation attempts ended (ready, unavailable, deferred)", ["source", "outcome"]
)
FEEDBACK_TIME_TO_READY = Histogram(
    "ai_feedback_time_to_ready_seconds",
    "From request creation to saved feedback: the eventual-consistency window",
    ["source"],
    buckets=(1, 2, 5, 10, 20, 30, 60, 120, 300, 600, 1800),
)
FEEDBACK_REQUESTS = Gauge(
    "ai_feedback_requests", "Feedback requests currently waiting, by status", ["status"]
)
GENERATIONS_IN_FLIGHT = Gauge("ai_generations_in_flight", "Generations running right now")
DIRECT_FEEDBACK = Counter(
    "ai_direct_feedback_total",
    "Direct hints for accepted submissions: ready in the request, or fell back to the queue",
    ["outcome"],
)
GENERATION_RETRIES = Counter(
    "ai_generation_retries_total", "Generation attempts retried after a transient error", ["error"]
)

# ---- the LLM ----

LLM_GENERATION_SECONDS = Histogram(
    "ai_llm_generation_duration_seconds",
    "Time Ollama took to generate one feedback",
    ["model"],
    buckets=(0.5, 1, 2, 4, 6, 8, 10, 15, 20, 30, 60, 120),
)
LLM_TOKENS = Counter("ai_llm_tokens_total", "Tokens used by the LLM", ["model", "direction"])

# ---- dependencies and circuit breakers ----

DEPENDENCY_CALLS = Counter(
    "ai_dependency_calls_total",
    "Calls to Ollama, Submission and Catalog: success, failure (counts toward the circuit), "
    "error (the caller's fault, e.g. 404) or rejected (circuit open, no call made)",
    ["dependency", "outcome"],
)
DEPENDENCY_LATENCY = Histogram(
    "ai_dependency_call_duration_seconds",
    "Duration of calls that reached the dependency",
    ["dependency"],
    buckets=(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120),
)
CIRCUIT_STATE = Gauge(
    "ai_circuit_breaker_state", "Circuit state: 0 closed, 1 half-open, 2 open", ["dependency"]
)
CIRCUIT_TRANSITIONS = Counter(
    "ai_circuit_breaker_transitions_total", "Circuit state changes", ["dependency", "to_state"]
)

STATE_VALUES = {CircuitState.CLOSED: 0, CircuitState.HALF_OPEN: 1, CircuitState.OPEN: 2}


def track_breaker(breaker: CircuitBreaker) -> None:
    """Report a breaker's state, transitions and calls."""
    CIRCUIT_STATE.labels(breaker.name).set(STATE_VALUES[breaker.state])

    def on_change(name: str, _old: CircuitState, new: CircuitState) -> None:
        CIRCUIT_STATE.labels(name).set(STATE_VALUES[new])
        CIRCUIT_TRANSITIONS.labels(name, new.value).inc()

    def on_call(name: str, outcome: str, seconds: float | None) -> None:
        DEPENDENCY_CALLS.labels(name, outcome).inc()
        if seconds is not None:
            DEPENDENCY_LATENCY.labels(name).observe(seconds)

    breaker.on_state_change(on_change)
    breaker.on_call(on_call)


def render() -> tuple[bytes, str]:
    return generate_latest(), CONTENT_TYPE_LATEST
