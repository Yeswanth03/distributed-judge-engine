import logging

from .broker import consume_forever
from .catalog_client import CatalogClient
from .config import settings
from .health_server import JOBS_PROCESSED, start_in_background
from .judge import judge_submission
from .run import run_code
from .spawner_client import SpawnerClient

logging.basicConfig(level=logging.INFO)


def main():
    start_in_background()
    catalog = CatalogClient()
    spawner = SpawnerClient()

    def judge_fn(job):
        verdict = judge_submission(job, catalog, spawner, settings.worker_id)
        JOBS_PROCESSED.labels(status=verdict.verdict).inc()
        return verdict

    def run_fn(job):
        return run_code(job, catalog, spawner)

    consume_forever(judge_fn, run_fn)


if __name__ == "__main__":
    main()
