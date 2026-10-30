from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="SPAWNER_")

    sandbox_image: str = "python:3.11-slim"

    # Pool shape (final decision, see docs/judge-worker-and-spawner-design.md):
    # 1 container always open, everything past that spawned on demand and reclaimed
    # after sitting idle for retention_seconds.
    floor_size: int = 1
    retention_seconds: int = 300  # 5 minutes
    reaper_interval_seconds: int = 15

    # TODO: size this from the real deploy host's memory/CPU budget before the demo,
    # not an arbitrary number. See "Open Questions" in the design doc for the formula.
    max_containers: int = 10

    # Hard ceiling applied to every pool container regardless of which problem it judges.
    # Must be >= the largest memory_limit_mb Catalog will ever publish for a problem.
    container_memory_mb: int = 512
    container_cpus: float = 1.0

    port: int = 8081


settings = Settings()
