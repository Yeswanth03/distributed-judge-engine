import logging

from .broker import consume_forever
from .models import FailedVerdictEvent

logging.basicConfig(level=logging.INFO)


def handle_failed_verdict(event: FailedVerdictEvent) -> None:
    """Part 1 handler: verify delivery and expose the submission identifier.

    Later parts will fetch submission/problem data, call Ollama, and persist the
    result before this handler returns (and therefore before the broker ack).
    """
    logger = logging.getLogger("ai_service")
    logger.info(
        "received failed verdict: submission_id=%s problem_id=%s verdict=%s rejudge_seq=%s",
        event.submission_id,
        event.problem_id,
        event.verdict.value,
        event.rejudge_seq,
    )


def main() -> None:
    consume_forever(handle_failed_verdict)


if __name__ == "__main__":
    main()
