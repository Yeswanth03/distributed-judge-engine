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
    # Code-specialized model: on a real Two Sum off-by-one bug it named the cause in
    # 3/3 runs, where llama3.2 gave wrong advice in every run. About 4.7 GB.
    ollama_model: str = "qwen2.5-coder:7b"
    ollama_timeout_seconds: float = 120.0
    prompt_version: str = "2"

    # Background generation: worker threads, attempts per request for transient
    # failures, and how often queued/stuck requests are picked up again.
    generation_workers: int = 2
    generation_attempts: int = 3
    sweep_interval_seconds: int = 30

    database_url: str = "postgresql://postgres:postgres@localhost:5432/ai_feedback"
    api_host: str = "0.0.0.0"
    api_port: int = 8084


settings = Settings()
