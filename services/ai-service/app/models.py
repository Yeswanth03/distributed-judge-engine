from datetime import datetime
from enum import StrEnum
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field


class FailureVerdict(StrEnum):
    WRONG_ANSWER = "wrong_answer"
    TIME_LIMIT_EXCEEDED = "time_limit_exceeded"
    MEMORY_LIMIT_EXCEEDED = "memory_limit_exceeded"
    RUNTIME_ERROR = "runtime_error"
    COMPILE_ERROR = "compile_error"


class FailedVerdictEvent(BaseModel):
    """The required verdict event fields published by the Judge Worker.

    Extra fields are ignored to remain compatible with the Judge Worker's
    richer event payload (test results, timing, worker id, and so on).
    """

    model_config = ConfigDict(extra="ignore")

    submission_id: UUID
    user_id: str = Field(min_length=1)
    problem_id: UUID
    testset_version: int = Field(ge=1)
    verdict: FailureVerdict
    rejudge_seq: int = Field(default=0, ge=0)
    judged_at: datetime
