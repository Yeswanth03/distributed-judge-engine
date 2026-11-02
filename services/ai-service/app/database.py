from pathlib import Path
from uuid import UUID, uuid4

import psycopg
from psycopg.rows import dict_row

from .config import settings


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

        for migration in sorted(migration_dir.glob("V*__*.sql")):
            version = migration.name.split("__", maxsplit=1)[0]
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
        problem_id: UUID,
        verdict: str,
        rejudge_seq: int,
        trigger: str,
        idempotency_key: str,
        initial_status: str = "queued",
    ) -> dict:
        request_id = uuid4()
        with self._connect() as connection:
            connection.execute(
                """
                INSERT INTO feedback_requests (
                    request_id, submission_id, user_id, problem_id, verdict,
                    rejudge_seq, request_source, idempotency_key, status,
                    model_name, prompt_version, input_tokens, output_tokens
                ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, NULL, NULL)
                ON CONFLICT (user_id, idempotency_key) DO NOTHING
                """,
                (
                    request_id,
                    submission_id,
                    user_id,
                    problem_id,
                    verdict,
                    rejudge_seq,
                    trigger,
                    idempotency_key,
                    initial_status,
                    settings.ollama_model,
                    settings.prompt_version,
                ),
            )
            return connection.execute(
                "SELECT * FROM feedback_requests WHERE user_id = %s AND idempotency_key = %s",
                (user_id, idempotency_key),
            ).fetchone()

    def mark_generating(self, request_id: UUID) -> None:
        with self._connect() as connection:
            connection.execute(
                "UPDATE feedback_requests SET status = 'generating', updated_at = now() WHERE request_id = %s",
                (request_id,),
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
                    output_tokens = %s, updated_at = now()
                WHERE request_id = %s
                RETURNING *
                """,
                (feedback, input_tokens, output_tokens, request_id),
            ).fetchone()

    def mark_unavailable(self, request_id: UUID) -> None:
        with self._connect() as connection:
            connection.execute(
                "UPDATE feedback_requests SET status = 'unavailable', updated_at = now() WHERE request_id = %s",
                (request_id,),
            )

    def get_request(self, request_id: UUID, user_id: str) -> dict | None:
        with self._connect() as connection:
            return connection.execute(
                "SELECT * FROM feedback_requests WHERE request_id = %s AND user_id = %s",
                (request_id, user_id),
            ).fetchone()

    def get_request_by_key(self, idempotency_key: str, user_id: str) -> dict | None:
        with self._connect() as connection:
            return connection.execute(
                "SELECT * FROM feedback_requests WHERE user_id = %s AND idempotency_key = %s",
                (user_id, idempotency_key),
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
