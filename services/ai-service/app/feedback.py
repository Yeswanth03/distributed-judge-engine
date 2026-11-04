"""Turns a feedback request into saved feedback.

Both entry points only create a 'queued' row and hand it to a background worker:
  - a failed verdict event from RabbitMQ (acked once the row is saved)
  - a user's on-demand POST (answered 202 straight away)
A periodic sweep re-submits queued rows and rows left in 'processing' by a crash,
so nothing is lost if the process dies between saving a row and finishing it.
"""

import logging
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from uuid import UUID

import httpx
import psycopg

from .clients import CatalogClient, ContextNotFound, SubmissionClient
from .config import settings
from .database import FeedbackDatabase, feedback_database
from .models import FailedVerdictEvent
from .ollama_client import OllamaClient
from .schemas import FeedbackContext

logger = logging.getLogger("ai_service.feedback")


class SubmissionNotJudged(Exception):
    """The submission has no final verdict yet, or the judge itself failed on it."""


def _first_set(*values):
    return next((value for value in values if value is not None), None)


class FeedbackProcessor:
    def __init__(
        self,
        database: FeedbackDatabase = feedback_database,
        submissions: SubmissionClient | None = None,
        catalog: CatalogClient | None = None,
        ollama: OllamaClient | None = None,
        retry_delay_seconds: float = 2.0,
    ) -> None:
        self.database = database
        self.submissions = submissions or SubmissionClient()
        self.catalog = catalog or CatalogClient()
        self.ollama = ollama or OllamaClient()
        self.retry_delay_seconds = retry_delay_seconds
        self._executor: ThreadPoolExecutor | None = None
        self._in_flight: set[UUID] = set()
        self._lock = threading.Lock()
        self._stop = threading.Event()

    # ---- entry points ----

    def process_failed_verdict(self, event: FailedVerdictEvent) -> dict:
        """Saves a queued request for the failed verdict. Redelivery of the same
        event (same submission and rejudge_seq) returns the existing row."""
        row = self.database.create_request(
            submission_id=event.submission_id,
            user_id=event.user_id,
            problem_id=event.problem_id,
            verdict=event.verdict.value,
            rejudge_seq=event.rejudge_seq,
            trigger="failed_verdict",
            idempotency_key=f"verdict:{event.submission_id}:{event.rejudge_seq}",
            judge_details=event.judge_details(),
        )
        logger.info("failed verdict queued: submission=%s request=%s status=%s",
                    event.submission_id, row["request_id"], row["status"])
        if row["status"] == "queued":
            self.submit(row["request_id"])
        return row

    def request_on_demand(self, submission_id: UUID, user_id: str) -> tuple[dict, bool]:
        """Checks the caller owns a judged submission, then queues (or reuses) a request.
        Raises ContextNotFound, SubmissionNotJudged, or httpx.HTTPError."""
        submission = self.submissions.get_submission(str(submission_id), user_id)
        if submission.verdict is None:
            raise SubmissionNotJudged("the submission has not been judged yet")
        if submission.verdict == "internal_error":
            raise SubmissionNotJudged("the judge could not run this submission; submit it again")

        row, created = self.database.create_or_reuse_user_request(
            submission_id=submission_id,
            user_id=user_id,
            problem_id=None,
            verdict=submission.verdict,
        )
        if row["status"] == "queued":
            self.submit(row["request_id"])
        return row, created

    # ---- generation ----

    def generate(self, request_id: UUID) -> None:
        row = self.database.claim(request_id)
        if row is None:
            return  # already done, or another worker is on it

        last_error = "unknown error"
        for attempt in range(1, settings.generation_attempts + 1):
            try:
                context = self._load_context(row)
                self.database.set_problem_id(request_id, context.problem.problem_id)
                generated = self.ollama.generate_feedback(context)
                self.database.save_feedback(
                    request_id, generated.text, generated.input_tokens, generated.output_tokens
                )
                logger.info("feedback ready: request=%s submission=%s", request_id, row["submission_id"])
                return
            except ContextNotFound as exc:
                logger.warning("feedback unavailable for request %s: %s", request_id, exc)
                self.database.mark_unavailable(request_id, str(exc))
                return
            except (httpx.HTTPError, ValueError, KeyError) as exc:
                last_error = f"{type(exc).__name__}: {exc}"
                logger.warning("attempt %s/%s failed for request %s: %s",
                               attempt, settings.generation_attempts, request_id, last_error)
                if attempt < settings.generation_attempts:
                    time.sleep(self.retry_delay_seconds * attempt)

        self.database.mark_unavailable(request_id, last_error)

    def _load_context(self, row: dict) -> FeedbackContext:
        details = row.get("judge_details") or {}
        submission = self.submissions.get_submission(str(row["submission_id"]), row["user_id"])
        problem = self.catalog.get_problem(submission.problem_ref, details.get("testset_version"))

        # The Submission Service may not have applied the verdict yet when the AI
        # service sees the same event, so its counters are only a fallback.
        judged = submission.verdict is not None
        return FeedbackContext(
            verdict=row["verdict"],
            submission=submission,
            problem=problem,
            test_cases_passed=_first_set(details.get("test_cases_passed"),
                                         submission.test_cases_passed if judged else None),
            total_test_cases=_first_set(details.get("total_test_cases"),
                                        submission.total_test_cases if judged else None),
            error_message=_first_set(details.get("error_message"),
                                     submission.error_message if judged else None),
        )

    # ---- background workers ----

    def start(self) -> None:
        self._executor = ThreadPoolExecutor(
            max_workers=settings.generation_workers, thread_name_prefix="feedback-generator"
        )
        threading.Thread(target=self._sweep_loop, name="feedback-sweeper", daemon=True).start()

    def stop(self) -> None:
        self._stop.set()
        if self._executor is not None:
            self._executor.shutdown(wait=False, cancel_futures=True)

    def submit(self, request_id: UUID) -> None:
        if self._executor is None:
            return  # not started (tests); the sweep picks it up once running
        with self._lock:
            if request_id in self._in_flight:
                return
            self._in_flight.add(request_id)
        self._executor.submit(self._run, request_id)

    def _run(self, request_id: UUID) -> None:
        try:
            self.generate(request_id)
        except psycopg.Error:
            # Database trouble: leave the row for the sweep to retry later.
            logger.exception("database error for request %s, will retry", request_id)
            try:
                self.database.requeue_for_retry(request_id)
            except Exception:
                logger.exception("could not requeue request %s", request_id)
        except Exception as exc:
            # A bug would fail the same way every time; stop instead of looping.
            logger.exception("generation crashed for request %s", request_id)
            try:
                self.database.mark_unavailable(request_id, f"internal error: {type(exc).__name__}")
            except Exception:
                logger.exception("could not mark request %s unavailable", request_id)
        finally:
            with self._lock:
                self._in_flight.discard(request_id)

    def _sweep_loop(self) -> None:
        stale_after = settings.ollama_timeout_seconds * settings.generation_attempts + 60
        while not self._stop.is_set():
            try:
                for request_id in self.database.pending_request_ids(stale_after):
                    self.submit(request_id)
            except Exception:
                logger.exception("feedback sweep failed, retrying next tick")
            self._stop.wait(settings.sweep_interval_seconds)


feedback_processor = FeedbackProcessor()
