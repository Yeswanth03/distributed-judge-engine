from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="AI_")

    rabbitmq_url: str = "amqp://guest:guest@localhost:5672/"
    verdicts_exchange: str = "verdicts"
    feedback_queue: str = "ai.feedback.jobs"
    dead_letter_exchange: str = "verdicts.dlx"
    dead_letter_queue: str = "ai.feedback.dlq"
    reconnect_max_seconds: int = 30
    ollama_base_url: str = "http://localhost:11434"
    ollama_model: str = "llama3.2"
    ollama_timeout_seconds: float = 120.0
    prompt_version: str = "1"
    database_url: str = "postgresql://postgres:postgres@localhost:5432/ai_feedback"
    api_host: str = "0.0.0.0"
    api_port: int = 8084


settings = Settings()
