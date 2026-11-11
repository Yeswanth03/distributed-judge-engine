from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="JUDGE_")

    rabbitmq_url: str = "amqp://judge:judge@localhost:5672/"
    # Must match deploy/rabbitmq/definitions.json: RabbitMQ rejects a declaration
    # whose arguments differ from the existing queue's with PRECONDITION_FAILED.
    jobs_queue: str = "jobs.queue"
    jobs_dlq: str = "jobs.dlq"
    dead_letter_exchange: str = "dead-letter"
    jobs_delivery_limit: int = 3
    # Trial runs ("Run" in the editor): their own queue in, and results straight
    # back to the Submission Service, never to the verdicts exchange.
    runs_queue: str = "runs.queue"
    runs_dlq: str = "runs.dlq"
    runs_delivery_limit: int = 3
    run_results_queue: str = "submission-service.run-results.queue"
    run_results_dlq: str = "submission-service.run-results.dlq"
    run_results_delivery_limit: int = 5
    # Must match Catalog's RabbitConfig.VERDICT_EXCHANGE exactly: a topic
    # exchange named "verdicts", bound by Catalog's queue on "verdict.#".
    verdicts_exchange: str = "verdicts"

    catalog_base_url: str = "http://localhost:8080"
    spawner_base_url: str = "http://localhost:8081"

    # Must match catalog.internal-token on the Catalog service (its default local
    # value is "local-dev-token", see services/catalog/src/main/resources/application-local.properties).
    internal_service_token: str = "local-dev-token"

    worker_id: str = "worker-1"
    # 8082 on the host belongs to the Submission Service (see docker-compose.yml).
    health_port: int = 8083


settings = Settings()
