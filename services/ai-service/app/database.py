from pathlib import Path
from uuid import UUID, uuid4

import psycopg
from psycopg.rows import dict_row
from psycopg.types.json import Jsonb

from .config import settings

# A request that is reused instead of starting a new one.
ACTIVE_STATUSES = ("queued", "processing", "ready")


class FeedbackDatabase:
    def _connect(self):
        return psycopg.connect(settings.database_url, row_factory=dict_row)

    def initialize(self) -> None:
        migration_dir = Path(__file__).resolve().parent.parent / "db" / "migration"
        with self._connect() as connection:
            connection.execute(
                """
                CREATE TABLE IF NOT EXISTS ai_schema_migrations (
                    version TEXT PRIMARY KEY,
                    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
                )
                """
            )

        for migration in sorted(migration_dir.glob("V*__*.sql"), key=lambda p: int(p.name[1:].split("__")[0])):
            version = migration.name.split("__", maxsplit=1)[0]
            # One transaction per migration: it applies completely or not at all.
            with self._connect() as connection:
                applied = connection.execute(
                    "SELECT 1 FROM ai_schema_migrations WHERE version = %s",
                    (version,),
                ).fetchone()
                if applied:
                    continue
                for statement in migration.read_text().split(";"):
                    if statement.strip():
                        connection.execute(statement)
                connection.execute(
                    "INSERT INTO ai_schema_migrations (version) VALUES (%s)",
                    (version,),
                )

    def create_request(
        self,
        *,
        submission_id: UUID,
        user_id: str,
        problem_id: str | None,
        verdict: str,
        rejudge_seq: int,
        trigger: str,
        idempotency_key: str,
        judge_details: dict | None = None,
    ) -> dict:
        """Inserts a queued request, or returns the existing one with the same key."""
        with self._connect() as connection:
            return self._insert(
                connection,
                submission_id=submission_id,
                user_id=user_id,
                problem_id=problem_id,
                verdict=verdict,
                rejudge_seq=rejudge_seq,
                trigger=trigger,
                idempotency_key=idempotency_key,
                judge_details=judge_details,
            )

    def create_or_reuse_user_request(
        self,
        *,
        submission_id: UUID,
        user_id: str,
        problem_id: str | None,
        verdict: str,
    ) -> tuple[dict, bool]:
        """Returns (row, created). Reuses the latest queued, processing or ready
        request for this user's submission, including automatic failed-verdict
        feedback. A new attempt is made only when there is none, e.g. after
        'unavailable'. The advisory lock stops a double click creating two."""
        with self._connect() as connection:
            connection.execute(
                "SELECT pg_advisory_xact_lock(hashtext(%s))",
                (f"{user_id}:{submission_id}",),
            )
            existing = connection.execute(
                """
                SELECT * FROM feedback_requests
                WHERE user_id = %s AND submission_id = %s AND status = ANY(%s)
                ORDER BY created_at DESC
                LIMIT 1
                """,
                (user_id, submission_id, list(ACTIVE_STATUSES)),
            ).fetchone()
            if existing:
                return existing, False
            row = self._insert(
                connection,
                submission_id=submission_id,
                user_id=user_id,
                problem_id=problem_id,
                verdict=verdict,
                rejudge_seq=0,
                trigger="user_request",
                idempotency_key=f"user:{submission_id}:{uuid4()}",
                judge_details=None,
            )
            return row, True

    @staticmethod
    def _insert(connection, *, submission_id, user_id, problem_id, verdict, rejudge_seq, trigger,
                idempotency_key, judge_details) -> dict:
        connection.execute(
            """
            INSERT INTO feedback_requests (
                request_id, submission_id, user_id, problem_id, verdict,
                rejudge_seq, request_source, idempotency_key, status,
                model_name, prompt_version, judge_details
            ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, 'queued', %s, %s, %s)
            ON CONFLICT (user_id, idempotency_key) DO NOTHING
            """,
            (
                uuid4(),
                submission_id,
                user_id,
                problem_id,
                verdict,
                rejudge_seq,
                trigger,
                idempotency_key,
                settings.ollama_model,
                settings.prompt_version,
                Jsonb(judge_details) if judge_details is not None else None,
            ),
        )
        return connection.execute(
            "SELECT * FROM feedback_requests WHERE user_id = %s AND idempotency_key = %s",
            (user_id, idempotency_key),
        ).fetchone()

    def claim(self, request_id: UUID) -> dict | None:
        """Moves a queued request to processing. None if another worker got it first
        or it is no longer queued, so a request is never generated twice at once."""
        with self._connect() as connection:
            return connection.execute(
                """
                UPDATE feedback_requests
                SET status = 'processing', updated_at = now()
                WHERE request_id = %s AND status = 'queued'
                RETURNING *
                """,
                (request_id,),
            ).fetchone()

    def requeue_for_retry(self, request_id: UUID) -> None:
        with self._connect() as connection:
            connection.execute(
                "UPDATE feedback_requests SET status = 'queued', updated_at = now() "
                "WHERE request_id = %s AND status = 'processing'",
                (request_id,),
            )

    def set_problem_id(self, request_id: UUID, problem_id: str) -> None:
        with self._connect() as connection:
            connection.execute(
                "UPDATE feedback_requests SET problem_id = %s WHERE request_id = %s",
                (problem_id, request_id),
            )

    def save_feedback(
        self,
        request_id: UUID,
        feedback: str,
        input_tokens: int | None,
        output_tokens: int | None,
    ) -> dict:
        with self._connect() as connection:
            return connection.execute(
                """
                UPDATE feedback_requests
                SET status = 'ready', feedback = %s, input_tokens = %s,
                    output_tokens = %s, error_reason = NULL, model_name = %s,
                    prompt_version = %s, updated_at = now()
                WHERE request_id = %s
                RETURNING *
                """,
                (feedback, input_tokens, output_tokens, settings.ollama_model,
                 settings.prompt_version, request_id),
            ).fetchone()

    def mark_unavailable(self, request_id: UUID, reason: str) -> None:
        with self._connect() as connection:
            connection.execute(
                "UPDATE feedback_requests SET status = 'unavailable', error_reason = %s, "
                "updated_at = now() WHERE request_id = %s",
                (reason[:500], request_id),
            )

    def pending_request_ids(self, stale_after_seconds: float, limit: int = 50) -> list[UUID]:
        """Queued requests, after returning ones stuck in processing (the process
        died mid-generation) to the queue."""
        with self._connect() as connection:
            connection.execute(
                """
                UPDATE feedback_requests SET status = 'queued', updated_at = now()
                WHERE status = 'processing' AND updated_at < now() - make_interval(secs => %s)
                """,
                (stale_after_seconds,),
            )
            rows = connection.execute(
                "SELECT request_id FROM feedback_requests WHERE status = 'queued' "
                "ORDER BY created_at LIMIT %s",
                (limit,),
            ).fetchall()
            return [row["request_id"] for row in rows]

    def get_request(self, request_id: UUID, user_id: str) -> dict | None:
        with self._connect() as connection:
            return connection.execute(
                "SELECT * FROM feedback_requests WHERE request_id = %s AND user_id = %s",
                (request_id, user_id),
            ).fetchone()

    def get_latest_for_submission(self, submission_id: UUID, user_id: str) -> dict | None:
        with self._connect() as connection:
            return connection.execute(
                """
                SELECT * FROM feedback_requests
                WHERE submission_id = %s AND user_id = %s
                ORDER BY created_at DESC
                LIMIT 1
                """,
                (submission_id, user_id),
            ).fetchone()


feedback_database = FeedbackDatabase()
