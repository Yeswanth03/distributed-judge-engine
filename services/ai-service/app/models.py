from datetime import datetime
from enum import StrEnum
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator


def normalize_verdict(value: str) -> str:
    """Judge Worker sends UPPERCASE verdicts (docs/contracts.md) under lowercase
    routing keys; older events used lowercase and "compile_error". Store one form."""
    verdict = value.strip().lower()
    return "compilation_error" if verdict == "compile_error" else verdict


class FailureVerdict(StrEnum):
    WRONG_ANSWER = "wrong_answer"
    TIME_LIMIT_EXCEEDED = "time_limit_exceeded"
    MEMORY_LIMIT_EXCEEDED = "memory_limit_exceeded"
    RUNTIME_ERROR = "runtime_error"
    COMPILATION_ERROR = "compilation_error"


class FailedVerdictEvent(BaseModel):
    """The fields of the Judge Worker's COMPLETED event that the AI service reads.

    Extra fields (test_results, worker_id, ...) are ignored. problem_id is kept as
    a string: the contract says Catalog UUID, but the worker currently forwards the
    job's slug. The AI service resolves the problem through the submission anyway.
    """

    model_config = ConfigDict(extra="ignore")

    submission_id: UUID
    user_id: str = Field(min_length=1, max_length=200)
    problem_id: str = Field(min_length=1, max_length=200)
    verdict: FailureVerdict
    rejudge_seq: int = Field(default=0, ge=0)
    testset_version: int | None = Field(default=None, ge=1)
    test_cases_passed: int | None = Field(default=None, ge=0)
    total_test_cases: int | None = Field(default=None, ge=0)
    error_message: str | None = Field(default=None, max_length=2000)
    judged_at: datetime | None = None

    @field_validator("verdict", mode="before")
    @classmethod
    def _normalize(cls, value):
        return normalize_verdict(value) if isinstance(value, str) else value

    def judge_details(self) -> dict:
        """What the judge reported, kept with the request so the prompt does not
        depend on the Submission Service having applied this verdict yet."""
        return {
            "testset_version": self.testset_version,
            "test_cases_passed": self.test_cases_passed,
            "total_test_cases": self.total_test_cases,
            "error_message": self.error_message,
        }
