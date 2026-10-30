import logging
import time

import pika

from .config import settings
from .models import Job, Verdict

logger = logging.getLogger("judge_worker.broker")

MAX_RECONNECT_DELAY_SECONDS = 30


def publish_verdict(channel, verdict: Verdict):
    # Routing key matches Catalog's real binding ("verdict.#", see
    # RabbitConfig.java) so Catalog's own queue actually receives this, and so
    # other consumers (e.g. the AI Service, which only wants failures) can bind
    # selectively on "verdict.wrong_answer" etc. instead of getting everything.
    channel.basic_publish(
        exchange=settings.verdicts_exchange,
        routing_key=f"verdict.{verdict.verdict}",
        body=verdict.model_dump_json().encode("utf-8"),
        properties=pika.BasicProperties(delivery_mode=2),  # persistent
    )


def consume_forever(judge_fn):
    """Reconnects with backoff on any broker disconnect, rather than letting
    the whole worker process exit. A real bug, not hypothetical: a brief
    RabbitMQ outage (Docker Desktop's idle auto-pause, in practice) tripped
    pika's heartbeat timeout and crashed the process entirely before this fix,
    which meant every in-flight job silently stopped being judged until
    someone noticed the process was gone and restarted it by hand. Now it just
    reconnects and keeps going.
    """
    delay = 1
    while True:
        try:
            _consume_once(judge_fn)
        except Exception:
            logger.exception("broker connection lost, reconnecting in %ss", delay)
            time.sleep(delay)
            delay = min(delay * 2, MAX_RECONNECT_DELAY_SECONDS)
        else:
            delay = 1  # a clean return (shouldn't normally happen) resets backoff


def _consume_once(judge_fn):
    """Blocking consume loop for a single connection's lifetime. Acks a job
    only after its verdict is durably published, never before, so a crash
    before publishing causes a safe redelivery instead of a silently lost job
    (see the crash-handling section of the design doc). A job that fails for
    infra reasons (Spawner unreachable, Catalog unreachable) is nacked with
    requeue=True rather than acked, so it comes back around instead of
    vanishing.
    """
    connection = pika.BlockingConnection(pika.URLParameters(settings.rabbitmq_url))
    channel = connection.channel()
    channel.queue_declare(queue=settings.jobs_queue, durable=True)
    # Declared idempotently here too (Catalog's RabbitConfig also declares it)
    # in case this worker starts before Catalog does. Must match exactly:
    # topic exchange named "verdicts", not the fanout "judge.verdicts" this
    # used to be, which would have meant Catalog's queue never got anything.
    channel.exchange_declare(exchange=settings.verdicts_exchange, exchange_type="topic", durable=True)
    channel.basic_qos(prefetch_count=1)

    def on_message(ch, method, _properties, body):
        try:
            job = Job.model_validate_json(body)
            verdict = judge_fn(job)
            publish_verdict(ch, verdict)
            ch.basic_ack(delivery_tag=method.delivery_tag)
        except Exception:
            logger.exception("job failed, leaving it unacked for redelivery")
            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=True)

    channel.basic_consume(queue=settings.jobs_queue, on_message_callback=on_message)
    logger.info("judge worker consuming from %s", settings.jobs_queue)
    try:
        channel.start_consuming()
    finally:
        try:
            connection.close()
        except Exception:
            pass
