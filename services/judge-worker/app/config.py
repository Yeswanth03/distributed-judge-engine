from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="JUDGE_")

    rabbitmq_url: str = "amqp://guest:guest@localhost:5672/"
    jobs_queue: str = "judge.jobs"
    # Must match Catalog's RabbitConfig.VERDICT_EXCHANGE exactly: a topic
    # exchange named "verdicts", bound by Catalog's queue on "verdict.#".
    verdicts_exchange: str = "verdicts"

    catalog_base_url: str = "http://localhost:8080"
    spawner_base_url: str = "http://localhost:8081"

    # Must match catalog.internal-token on the Catalog service (its default local
    # value is "local-dev-token", see services/catalog/src/main/resources/application-local.properties).
    internal_service_token: str = "local-dev-token"

    worker_id: str = "worker-1"
    health_port: int = 8082


settings = Settings()
