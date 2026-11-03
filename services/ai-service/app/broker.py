import logging
import time
from collections.abc import Callable

import pika
from pydantic import ValidationError

from .config import settings
from .models import FailedVerdictEvent, normalize_verdict

logger = logging.getLogger("ai_service.broker")

# The Judge Worker publishes verdict.<verdict> in lowercase (docs/contracts.md).
# verdict.compile_error is the older name, kept so either form is received.
FAILURE_ROUTING_KEYS = (
    "verdict.wrong_answer",
    "verdict.time_limit_exceeded",
    "verdict.memory_limit_exceeded",
    "verdict.runtime_error",
    "verdict.compilation_error",
    "verdict.compile_error",
)


def declare_topology(channel) -> None:
    """Declare a private durable queue bound to failed verdicts only. The exchanges
    already exist from deploy/rabbitmq/definitions.json; re-declaring them with the
    same arguments is harmless and lets the service start against a bare broker."""
    channel.exchange_declare(
        exchange=settings.verdicts_exchange,
        exchange_type="topic",
        durable=True,
    )
    channel.exchange_declare(
        exchange=settings.dead_letter_exchange,
        exchange_type="direct",
        durable=True,
    )
    channel.queue_declare(queue=settings.dead_letter_queue, durable=True)
    channel.queue_bind(
        queue=settings.dead_letter_queue,
        exchange=settings.dead_letter_exchange,
        routing_key=settings.feedback_queue,
    )
    channel.queue_declare(
        queue=settings.feedback_queue,
        durable=True,
        arguments={
            "x-dead-letter-exchange": settings.dead_letter_exchange,
            "x-dead-letter-routing-key": settings.feedback_queue,
        },
    )
    for routing_key in FAILURE_ROUTING_KEYS:
        channel.queue_bind(
            queue=settings.feedback_queue,
            exchange=settings.verdicts_exchange,
            routing_key=routing_key,
        )


def _on_message(process_event: Callable[[FailedVerdictEvent], object]):
    def callback(channel, method, _properties, body: bytes) -> None:
        try:
            event = FailedVerdictEvent.model_validate_json(body)
        except (ValidationError, ValueError):
            # A malformed or ineligible payload cannot be fixed by retrying.
            logger.exception("rejecting invalid verdict event to the AI dead-letter queue")
            channel.basic_nack(delivery_tag=method.delivery_tag, requeue=False)
            return

        routed_verdict = normalize_verdict(method.routing_key.removeprefix("verdict."))
        if routed_verdict != event.verdict.value:
            logger.error(
                "routing key %s does not match event verdict %s; dead-lettering",
                method.routing_key,
                event.verdict.value,
            )
            channel.basic_nack(delivery_tag=method.delivery_tag, requeue=False)
            return

        try:
            # Ack once the request row is saved. Generation runs in the background
            # (it can take longer than the broker heartbeat), and the sweep resumes
            # any queued row if the process dies before finishing it.
            process_event(event)
            channel.basic_ack(delivery_tag=method.delivery_tag)
        except Exception:
            logger.exception(
                "failed saving request for submission %s; requeueing verdict",
                event.submission_id,
            )
            time.sleep(1)  # avoid a hot redelivery loop while the database is down
            channel.basic_nack(delivery_tag=method.delivery_tag, requeue=True)

    return callback


def _consume_once(process_event: Callable[[FailedVerdictEvent], object]) -> None:
    connection = pika.BlockingConnection(pika.URLParameters(settings.rabbitmq_url))
    try:
        channel = connection.channel()
        declare_topology(channel)
        channel.basic_qos(prefetch_count=1)
        channel.basic_consume(
            queue=settings.feedback_queue,
            on_message_callback=_on_message(process_event),
            auto_ack=False,
        )
        logger.info(
            "AI consumer listening on %s for %s",
            settings.feedback_queue,
            ", ".join(FAILURE_ROUTING_KEYS),
        )
        channel.start_consuming()
    finally:
        if connection.is_open:
            connection.close()


def consume_forever(process_event: Callable[[FailedVerdictEvent], object]) -> None:
    """Reconnect to RabbitMQ with bounded exponential backoff."""
    delay = 1
    while True:
        try:
            _consume_once(process_event)
            delay = 1
        except Exception:
            logger.exception("RabbitMQ connection failed; retrying in %ss", delay)
            time.sleep(delay)
            delay = min(delay * 2, settings.reconnect_max_seconds)
