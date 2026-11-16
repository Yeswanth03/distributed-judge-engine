"""Circuit breakers and retry backoff for the services the AI service depends on.

Each downstream dependency (Ollama, Submission Service, Catalog) gets its own
breaker, so one failing dependency never stops calls to the others.

    CLOSED     calls go through; the last `window_size` outcomes are remembered.
               `failure_threshold` failures among them -> OPEN.
    OPEN       calls fail immediately with CircuitOpenError, without touching the
               dependency, for `open_seconds`. This is what stops a dead dependency
               tying up worker threads and stops retries hammering it.
    HALF_OPEN  after `open_seconds`, one trial call is let through.
               success -> CLOSED (history cleared); failure -> OPEN again.

Only failures that say something about the dependency's health count: connection
errors, timeouts, HTTP 5xx and 429. A 404 for a missing submission is the caller's
problem, not an outage, so it never opens a circuit.
"""

import logging
import random
import threading
import time
from collections import deque
from collections.abc import Callable
from enum import StrEnum
from typing import TypeVar

import httpx

from .config import settings

logger = logging.getLogger("ai_service.resilience")

T = TypeVar("T")


class CircuitState(StrEnum):
    CLOSED = "closed"
    OPEN = "open"
    HALF_OPEN = "half_open"


class CircuitOpenError(Exception):
    """The dependency's circuit is open: the call was refused without being made."""

    def __init__(self, dependency: str, retry_after: float) -> None:
        self.dependency = dependency
        self.retry_after = max(0.0, retry_after)
        super().__init__(f"{dependency} circuit is open; retry in {self.retry_after:.0f}s")


def is_dependency_failure(exc: BaseException) -> bool:
    """True when the error means the dependency is unhealthy (not that the request was bad)."""
    if isinstance(exc, httpx.TransportError):  # connection refused, DNS, timeouts
        return True
    if isinstance(exc, httpx.HTTPStatusError):
        code = exc.response.status_code
        return code >= 500 or code == 429
    return False


class CircuitBreaker:
    def __init__(
        self,
        name: str,
        failure_threshold: int = 5,
        window_size: int = 10,
        open_seconds: float = 10.0,
        is_failure: Callable[[BaseException], bool] = is_dependency_failure,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        if not 1 <= failure_threshold <= window_size:
            raise ValueError("failure_threshold must be between 1 and window_size")
        self.name = name
        self.failure_threshold = failure_threshold
        self.open_seconds = open_seconds
        self._is_failure = is_failure
        self._clock = clock
        self._lock = threading.Lock()
        self._outcomes: deque[bool] = deque(maxlen=window_size)  # True = failure
        self._state = CircuitState.CLOSED
        self._opened_at = 0.0
        self._trial_in_flight = False
        self._listeners: list[Callable[[str, CircuitState, CircuitState], None]] = []

    # ---- inspection ----

    @property
    def state(self) -> CircuitState:
        with self._lock:
            return self._current_state()

    def snapshot(self) -> dict:
        with self._lock:
            state = self._current_state()
            retry_after = (
                max(0.0, self._opened_at + self.open_seconds - self._clock())
                if state is CircuitState.OPEN else 0.0
            )
            return {
                "state": state.value,
                "recent_failures": sum(self._outcomes),
                "window": len(self._outcomes),
                "retry_after_seconds": round(retry_after, 1),
            }

    def on_state_change(self, listener: Callable[[str, CircuitState, CircuitState], None]) -> None:
        """listener(name, old_state, new_state); used for logging now and metrics later."""
        self._listeners.append(listener)

    # ---- the guarded call ----

    def call(self, fn: Callable[[], T]) -> T:
        is_trial = self._before_call()
        try:
            result = fn()
        except BaseException as exc:
            self._after_call(failed=self._is_failure(exc), is_trial=is_trial)
            raise
        self._after_call(failed=False, is_trial=is_trial)
        return result

    def _before_call(self) -> bool:
        """Raises CircuitOpenError if the call must not be made; returns True for the half-open trial."""
        with self._lock:
            state = self._current_state()
            if state is CircuitState.OPEN:
                raise CircuitOpenError(self.name, self._opened_at + self.open_seconds - self._clock())
            if state is CircuitState.HALF_OPEN:
                if self._trial_in_flight:
                    # Only one trial at a time; everyone else keeps failing fast.
                    raise CircuitOpenError(self.name, self.open_seconds)
                self._trial_in_flight = True
                return True
            return False

    def _after_call(self, failed: bool, is_trial: bool) -> None:
        with self._lock:
            if is_trial:
                # The trial decides: close and forget old failures, or open again.
                self._trial_in_flight = False
                if failed:
                    self._transition(CircuitState.OPEN)
                else:
                    self._outcomes.clear()
                    self._transition(CircuitState.CLOSED)
                return
            # A call that started while CLOSED; its outcome only counts if still CLOSED.
            if self._current_state() is not CircuitState.CLOSED:
                return
            self._outcomes.append(failed)
            if sum(self._outcomes) >= self.failure_threshold:
                self._transition(CircuitState.OPEN)

    def _current_state(self) -> CircuitState:
        """OPEN becomes HALF_OPEN by itself once open_seconds have passed."""
        if self._state is CircuitState.OPEN and self._clock() - self._opened_at >= self.open_seconds:
            self._transition(CircuitState.HALF_OPEN)
        return self._state

    def _transition(self, new: CircuitState) -> None:
        old, self._state = self._state, new
        if new is CircuitState.OPEN:
            self._opened_at = self._clock()
        logger.warning("circuit %s: %s -> %s", self.name, old.value, new.value)
        for listener in self._listeners:
            try:
                listener(self.name, old, new)
            except Exception:
                logger.exception("circuit listener failed")


def backoff_delay(attempt: int, base: float, cap: float) -> float:
    """Exponential backoff with jitter for retry number `attempt` (1, 2, 3...).

    The ceiling doubles each time (base, 2*base, 4*base...) up to `cap`, and the
    actual wait is random in [ceiling/2, ceiling]. The randomness spreads retries
    from many requests out, so they don't all hit a recovering dependency at the
    same instant (a "thundering herd")."""
    ceiling = min(cap, base * 2 ** (attempt - 1))
    return random.uniform(ceiling / 2, ceiling)


def _ollama_failure(exc: BaseException) -> bool:
    # An empty or malformed generation also means the model server is unhealthy.
    return is_dependency_failure(exc) or isinstance(exc, ValueError)


def new_breaker(name: str, **overrides) -> CircuitBreaker:
    options = {
        "failure_threshold": settings.breaker_failure_threshold,
        "window_size": settings.breaker_window_size,
        "open_seconds": settings.breaker_open_seconds,
        **({"is_failure": _ollama_failure} if name == "ollama" else {}),
        **overrides,
    }
    return CircuitBreaker(name, **options)


# One breaker per dependency, shared by every thread in the process.
breakers: dict[str, CircuitBreaker] = {name: new_breaker(name) for name in ("ollama", "submission", "catalog")}
