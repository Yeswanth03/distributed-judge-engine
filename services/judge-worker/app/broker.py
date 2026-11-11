import logging
import time

import pika

from .config import settings
from .models import Job, RunJob, RunningEvent, RunResult, Verdict

logger = logging.getLogger("judge_worker.broker")

MAX_RECONNECT_DELAY_SECONDS = 30


def publish_verdict(channel, verdict: Verdict):
    # Routing key matches Catalog's real binding ("verdict.#", see
    # RabbitConfig.java) so Catalog's own queue actually receives this, and so
    # other consumers (e.g. the AI Service, which only wants failures) can bind
    # selectively on "verdict.wrong_answer" etc. instead of getting everything.
    _publish(channel, f"verdict.{verdict.verdict.lower()}", verdict)


def publish_running(channel, event: RunningEvent):
    # Not under "verdict.": Catalog would dead-letter it as an incomplete verdict.
    _publish(channel, "status.running", event)


def _publish(channel, routing_key: str, event: RunningEvent | Verdict):
    channel.basic_publish(
        exchange=settings.verdicts_exchange,
        routing_key=routing_key,
        body=event.model_dump_json().encode("utf-8"),
        properties=pika.BasicProperties(
            delivery_mode=2,  # persistent
            content_type="application/json",
            message_id=event.submission_id,
        ),
    )


def publish_run_result(channel, result: RunResult):
    # Straight to the Submission Service's queue through the default exchange,
    # never the verdicts exchange: a run must not reach Catalog or the AI Service.
    # mandatory=True makes a missing queue an error instead of a silent drop.
    channel.basic_publish(
        exchange="",
        routing_key=settings.run_results_queue,
        body=result.model_dump_json().encode("utf-8"),
        properties=pika.BasicProperties(
            delivery_mode=2,  # persistent
            content_type="application/json",
            message_id=result.run_id,
        ),
        mandatory=True,
    )


def _declare_quorum_queue(channel, queue: str, dlq: str, delivery_limit: int):
    channel.queue_declare(queue=dlq, durable=True, arguments={"x-queue-type": "quorum"})
    channel.queue_bind(queue=dlq, exchange=settings.dead_letter_exchange, routing_key=dlq)
    channel.queue_declare(
        queue=queue,
        durable=True,
        arguments={
            "x-queue-type": "quorum",
            "x-delivery-limit": delivery_limit,
            "x-dead-letter-exchange": settings.dead_letter_exchange,
            "x-dead-letter-routing-key": dlq,
        },
    )


def declare_topology(channel):
    """Idempotent. The definitions file normally creates all of this first;
    declaring it here too means the worker also starts cleanly against a broker
    that was set up without that file."""
    channel.exchange_declare(exchange=settings.dead_letter_exchange, exchange_type="direct", durable=True)
    _declare_quorum_queue(channel, settings.jobs_queue, settings.jobs_dlq, settings.jobs_delivery_limit)
    _declare_quorum_queue(channel, settings.runs_queue, settings.runs_dlq, settings.runs_delivery_limit)
    _declare_quorum_queue(
        channel, settings.run_results_queue, settings.run_results_dlq, settings.run_results_delivery_limit
    )
    # Must match Catalog's RabbitConfig.java exactly: topic exchange "verdicts".
    channel.exchange_declare(exchange=settings.verdicts_exchange, exchange_type="topic", durable=True)


def consume_forever(judge_fn, run_fn):
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
            _consume_once(judge_fn, run_fn)
        except Exception:
            logger.exception("broker connection lost, reconnecting in %ss", delay)
            time.sleep(delay)
            delay = min(delay * 2, MAX_RECONNECT_DELAY_SECONDS)
        else:
            delay = 1  # a clean return (shouldn't normally happen) resets backoff


def _consume_once(judge_fn, run_fn):
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
    declare_topology(channel)
    # Publishes now raise if the broker refuses a message, so a verdict it never
    # took can't be acked away (the job gets nacked and redelivered instead).
    channel.confirm_delivery()
    channel.basic_qos(prefetch_count=1)

    def on_message(ch, method, _properties, body):
        try:
            job = Job.model_validate_json(body)
            publish_running(
                ch,
                RunningEvent(submission_id=job.submission_id, user_id=job.user_id, problem_id=job.problem_id),
            )
            verdict = judge_fn(job)
            publish_verdict(ch, verdict)
            ch.basic_ack(delivery_tag=method.delivery_tag)
        except Exception:
            logger.exception("job failed, leaving it unacked for redelivery")
            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=True)

    def on_run_message(ch, method, _properties, body):
        # Same ack-after-publish rule as a submission: a crash before the result
        # is safely with the broker means a redelivery, never a lost run.
        try:
            result = run_fn(RunJob.model_validate_json(body))
            publish_run_result(ch, result)
            ch.basic_ack(delivery_tag=method.delivery_tag)
        except Exception:
            logger.exception("run failed, leaving it unacked for redelivery")
            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=True)

    channel.basic_consume(queue=settings.jobs_queue, on_message_callback=on_message)
    channel.basic_consume(queue=settings.runs_queue, on_message_callback=on_run_message)
    logger.info("judge worker consuming from %s and %s", settings.jobs_queue, settings.runs_queue)
    try:
        channel.start_consuming()
    finally:
        try:
            connection.close()
        except Exception:
            pass
