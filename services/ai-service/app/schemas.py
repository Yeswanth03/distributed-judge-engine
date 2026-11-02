from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field


class ProblemDetailsInput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    problem_id: UUID
    title: str = Field(min_length=1, max_length=300)
    statement: str = Field(min_length=1, max_length=20000)
    constraints: str = Field(default="", max_length=10000)


class FeedbackRequestBody(BaseModel):
    """Swagger/local input; production should fetch this context internally."""

    model_config = ConfigDict(
        extra="forbid",
        json_schema_extra={
            "example": {
                "verdict": "wrong_answer",
                "language": "python3.11",
                "source_code": "def two_sum(nums, target):\n    return []",
                "problem": {
                    "problem_id": "db2353a5-0000-4000-8000-000000000001",
                    "title": "Two Sum",
                    "statement": "Return the indices of two values that add up to the target.",
                    "constraints": "The input list may contain duplicate values.",
                },
            }
        },
    )

    verdict: Literal[
        "accepted",
        "wrong_answer",
        "time_limit_exceeded",
        "memory_limit_exceeded",
        "runtime_error",
        "compile_error",
    ]
    language: str = Field(min_length=1, max_length=50)
    source_code: str = Field(min_length=1, max_length=65536)
    problem: ProblemDetailsInput


class SubmissionContext(BaseModel):
    submission_id: UUID
    user_id: str
    problem_id: UUID
    language: str
    source_code: str
    problem_title: str
    problem_statement: str
    constraints: str


class FeedbackResponse(BaseModel):
    request_id: UUID
    submission_id: UUID
    problem_id: UUID
    verdict: str
    rejudge_seq: int
    status: Literal["awaiting_context", "queued", "generating", "ready", "unavailable"]
    feedback: str | None
    model_name: str
    prompt_version: str
    input_tokens: int | None
    output_tokens: int | None
    created_at: datetime
    updated_at: datetime
