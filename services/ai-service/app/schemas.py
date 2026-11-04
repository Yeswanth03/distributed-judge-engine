from dataclasses import dataclass
from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import BaseModel

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


class FeedbackResponse(BaseModel):
    request_id: UUID
    submission_id: UUID
    problem_id: str | None
    verdict: str
    rejudge_seq: int
    request_source: Literal["failed_verdict", "user_request"]
    status: RequestStatus
    feedback: str | None
    error_reason: str | None
    model_name: str
    prompt_version: str
    input_tokens: int | None
    output_tokens: int | None
    created_at: datetime
    updated_at: datetime
