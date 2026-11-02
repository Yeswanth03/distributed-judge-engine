from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import BaseModel


class FeedbackResponse(BaseModel):
    request_id: UUID
    submission_id: UUID
    problem_id: UUID
    verdict: str
    rejudge_seq: int
    status: Literal["queued", "generating", "ready", "unavailable"]
    feedback: str | None
    model_name: str
    prompt_version: str
    input_tokens: int | None
    output_tokens: int | None
    created_at: datetime
    updated_at: datetime
