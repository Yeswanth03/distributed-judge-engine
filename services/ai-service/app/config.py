from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="AI_")

    # RabbitMQ. The "judge" login and the "verdicts" topic exchange come from
    # deploy/rabbitmq/definitions.json (see docs/contracts.md).
    rabbitmq_url: str = "amqp://judge:judge@localhost:5672/"
    verdicts_exchange: str = "verdicts"
    feedback_queue: str = "ai.feedback.jobs"
    dead_letter_exchange: str = "verdicts.dlx"
    dead_letter_queue: str = "ai.feedback.dlq"
    reconnect_max_seconds: int = 30

    # Submission Service: source code, language and problem slug of a submission.
    # 8082 on the host (see docker-compose.yml).
    submission_base_url: str = "http://localhost:8082"
    # Catalog: problem statement, limits and test cases.
    catalog_base_url: str = "http://localhost:8080"
    # Must match catalog.internal-token on Catalog (local default "local-dev-token").
    internal_service_token: str = "local-dev-token"
    http_timeout_seconds: float = 5.0

    ollama_base_url: str = "http://localhost:11434"
    ollama_model: str = "llama3.2"
    ollama_timeout_seconds: float = 120.0
    prompt_version: str = "2"

    # Background generation: worker threads, attempts per request for transient
    # failures, and how often queued/stuck requests are picked up again.
    generation_workers: int = 2
    generation_attempts: int = 3
    sweep_interval_seconds: int = 30

    # Retry backoff: waits grow 1 s, 2 s, 4 s... (with jitter), never above the cap.
    retry_base_seconds: float = 1.0
    retry_max_seconds: float = 8.0

    # Circuit breaker per dependency (Ollama, Submission, Catalog): this many
    # failures among the last `window_size` calls opens it; after `open_seconds`
    # one trial call is allowed through.
    breaker_failure_threshold: int = 5
    breaker_window_size: int = 10
    breaker_open_seconds: float = 10.0
    # A request waiting on an open circuit gives up (unavailable) after this long.
    max_defer_seconds: int = 1800

    database_url: str = "postgresql://postgres:postgres@localhost:5432/ai_feedback"
    api_host: str = "0.0.0.0"
    api_port: int = 8084


settings = Settings()
