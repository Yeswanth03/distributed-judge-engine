import logging

from .broker import consume_forever
from .mock_submission import MockSubmissionProvider
from .models import FailedVerdictEvent
from .ollama_client import OllamaClient

logging.basicConfig(level=logging.INFO)


def handle_failed_verdict(
    event: FailedVerdictEvent,
    submissions: MockSubmissionProvider,
    ollama: OllamaClient,
) -> None:
    """Fetch mock context and generate feedback before acknowledging the event."""
    logger = logging.getLogger("ai_service")
    context = submissions.fetch(event)
    feedback = ollama.generate_feedback(event, context)
    logger.info("AI feedback for submission %s: %s", event.submission_id, feedback)


def main() -> None:
    submissions = MockSubmissionProvider()
    ollama = OllamaClient()

    def process(event: FailedVerdictEvent) -> None:
        handle_failed_verdict(event, submissions, ollama)

    consume_forever(process)


if __name__ == "__main__":
    main()
