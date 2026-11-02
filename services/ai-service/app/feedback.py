import logging
from uuid import UUID

from .database import FeedbackDatabase, feedback_database
from .models import FailedVerdictEvent
from .ollama_client import OllamaClient
from .schemas import SubmissionContext

logger = logging.getLogger("ai_service.feedback")


class FeedbackProcessor:
    def __init__(
        self,
        database: FeedbackDatabase = feedback_database,
        ollama: OllamaClient | None = None,
    ) -> None:
        self.database = database
        self.ollama = ollama or OllamaClient()

    def request_feedback(
        self,
        *,
        submission_id: UUID,
        user_id: str,
        idempotency_key: str,
        context: SubmissionContext,
        verdict: str,
        rejudge_seq: int = 0,
        trigger: str = "user_request",
    ) -> dict:
        row = self.database.create_request(
            submission_id=submission_id,
            user_id=user_id,
            problem_id=context.problem_id,
            verdict=verdict,
            rejudge_seq=rejudge_seq,
            trigger=trigger,
            idempotency_key=idempotency_key,
        )
        if row["status"] == "ready":
            return row

        self.database.mark_generating(row["request_id"])
        try:
            generated = self.ollama.generate_feedback(verdict, context)
            return self.database.save_feedback(
                row["request_id"],
                generated.text,
                generated.input_tokens,
                generated.output_tokens,
            )
        except Exception:
            self.database.mark_unavailable(row["request_id"])
            raise

    def process_failed_verdict(self, event: FailedVerdictEvent) -> None:
        idempotency_key = f"verdict:{event.submission_id}:{event.rejudge_seq}"
        row = self.database.create_request(
            submission_id=event.submission_id,
            user_id=event.user_id,
            problem_id=event.problem_id,
            verdict=event.verdict.value,
            rejudge_seq=event.rejudge_seq,
            trigger="failed_verdict",
            idempotency_key=idempotency_key,
            initial_status="awaiting_context",
        )
        logger.info(
            "failed verdict saved; awaiting source/problem lookup: submission=%s request=%s status=%s",
            event.submission_id,
            row["request_id"],
            row["status"],
        )


feedback_processor = FeedbackProcessor()
