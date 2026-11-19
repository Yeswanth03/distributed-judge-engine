from dataclasses import dataclass
from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import BaseModel, model_validator

from .clients import ProblemDetails, SubmissionDetails

RequestStatus = Literal["queued", "processing", "ready", "unavailable"]


@dataclass(frozen=True)
class FeedbackContext:
    """Everything the prompt is built from, fetched from Submission Service and Catalog."""

    verdict: str
    submission: SubmissionDetails
    problem: ProblemDetails
    test_cases_passed: int | None
    total_test_cases: int | None
    error_message: str | None


# What a learner sees. The technical reason (error_reason) stays in the database and the
# logs for developers: it can name internal hosts and libraries, so it is never sent out.
TUTOR_WAITING = ("The AI tutor is temporarily unavailable. "
                 "Your hints will appear here automatically when it's back.")
TUTOR_FAILED = "The AI tutor couldn't produce hints right now. Please try again in a few minutes."
NOT_AVAILABLE = "Hints aren't available for this submission."
TAKING_LONGER = "Your hints are taking a little longer than usual. They'll appear here shortly."
# ContextNotFound reasons (clients.py) always start with the missing thing: "submission ..."
# or "problem ...". Technical errors start with an exception name, e.g. "HTTPStatusError: ...
# 404 Not Found", so matching on "not found" alone would misread a missing model.
_NOT_FOUND_PREFIXES = ("submission ", "problem ")


def student_message(status: str | None, error_reason: str | None) -> str | None:
    """A short, safe explanation for the learner, or None when the status says it all."""
    reason = (error_reason or "").lower()
    if status in ("queued", "processing") and reason.startswith("waiting"):
        return TUTOR_WAITING  # deferred: a circuit is open, the request completes on its own later
    if status in ("queued", "processing") and reason.startswith("generating in the background"):
        return TAKING_LONGER  # "Get hints" took longer than its limit and moved to the queue
    if status == "unavailable":
        return NOT_AVAILABLE if reason.startswith(_NOT_FOUND_PREFIXES) else TUTOR_FAILED
    return None


class FeedbackResponse(BaseModel):
    request_id: UUID
    submission_id: UUID
    problem_id: str | None
    verdict: str
    rejudge_seq: int
    request_source: Literal["failed_verdict", "user_request"]
    status: RequestStatus
    feedback: str | None
    message: str | None = None
    model_name: str
    prompt_version: str
    input_tokens: int | None
    output_tokens: int | None
    created_at: datetime
    updated_at: datetime

    @model_validator(mode="before")
    @classmethod
    def _add_student_message(cls, data):
        # Endpoints return database rows; the message is derived from status and error_reason.
        if isinstance(data, dict):
            data = {**data, "message": student_message(data.get("status"), data.get("error_reason"))}
        return data
