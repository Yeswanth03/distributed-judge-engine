import logging
from uuid import UUID

from .database import FeedbackDatabase, feedback_database
from .mock_submission import MockSubmissionProvider
from .models import FailedVerdictEvent
from .ollama_client import OllamaClient

logger = logging.getLogger("ai_service.feedback")


class FeedbackProcessor:
    def __init__(
        self,
        database: FeedbackDatabase = feedback_database,
        submissions: MockSubmissionProvider | None = None,
        ollama: OllamaClient | None = None,
    ) -> None:
        self.database = database
        self.submissions = submissions or MockSubmissionProvider()
        self.ollama = ollama or OllamaClient()

    def request_feedback(
        self,
        *,
        submission_id: UUID,
        user_id: str,
        verdict: str,
        rejudge_seq: int,
        trigger: str,
        idempotency_key: str,
        problem_id: UUID | None = None,
    ) -> dict:
        context = self.submissions.fetch(submission_id, user_id, problem_id)
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
        row = self.request_feedback(
            submission_id=event.submission_id,
            user_id=event.user_id,
            problem_id=event.problem_id,
            verdict=event.verdict.value,
            rejudge_seq=event.rejudge_seq,
            trigger="failed_verdict",
            idempotency_key=idempotency_key,
        )
        logger.info(
            "feedback status=%s for submission=%s request=%s",
            row["status"],
            event.submission_id,
            row["request_id"],
        )


feedback_processor = FeedbackProcessor()
