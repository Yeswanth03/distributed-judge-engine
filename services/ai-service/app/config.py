from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="AI_")

    rabbitmq_url: str = "amqp://guest:guest@localhost:5672/"
    verdicts_exchange: str = "verdicts"
    feedback_queue: str = "ai.feedback.jobs"
    dead_letter_exchange: str = "verdicts.dlx"
    dead_letter_queue: str = "ai.feedback.dlq"
    reconnect_max_seconds: int = 30


settings = Settings()
