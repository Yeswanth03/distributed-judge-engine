"""Publishes one real job onto RabbitMQ and waits for the verdict the real
Judge Worker publishes back, proving the whole chain actually works: broker ->
Judge Worker -> Catalog -> Spawner -> a real Docker container -> verdict ->
broker.

Needs, all already running: RabbitMQ (docker compose up -d rabbitmq), the fake
or real Catalog on :8080, the Spawner on :8081, the Judge Worker on :8082 (see
scripts/dev/README.md for the full runbook).

Run: python scripts/dev/e2e_smoke_test.py
"""

import json
import time
import uuid

import pika

params = pika.URLParameters("amqp://guest:guest@localhost:5672/")
conn = pika.BlockingConnection(params)
ch = conn.channel()

ch.queue_declare(queue="judge.jobs", durable=True)
# Must match Catalog's RabbitConfig.java exactly: topic exchange "verdicts".
ch.exchange_declare(exchange="verdicts", exchange_type="topic", durable=True)

# A temporary, exclusive queue bound to every verdict routing key ("verdict.#",
# same pattern Catalog's own queue binds on), just for this listener.
result = ch.queue_declare(queue="", exclusive=True)
listen_queue = result.method.queue
ch.queue_bind(exchange="verdicts", queue=listen_queue, routing_key="verdict.#")

job = {
    "submission_id": str(uuid.uuid4()),
    "problem_id": "11111111-1111-1111-1111-111111111111",
    "user_id": "user-1",
    "language": "python3.11",
    "source_code": "a, b = map(int, input().split())\nprint(a + b)",
    "submitted_at": "2026-10-29T00:00:00Z",
}

print("publishing job:", job["submission_id"])
ch.basic_publish(
    exchange="",
    routing_key="judge.jobs",
    body=json.dumps(job).encode("utf-8"),
    properties=pika.BasicProperties(delivery_mode=2),
)

print("waiting for verdict...")
deadline = time.time() + 15
verdict = None
while time.time() < deadline:
    method, _props, body = ch.basic_get(queue=listen_queue, auto_ack=True)
    if method:
        verdict = json.loads(body)
        break
    time.sleep(0.3)

if verdict is None:
    print("FAILED: no verdict received within timeout")
    raise SystemExit(1)

print("VERDICT RECEIVED:")
print(json.dumps(verdict, indent=2))
assert verdict["verdict"] == "accepted", f"expected accepted, got {verdict['verdict']}"
print("\nOK: end-to-end chain works.")

conn.close()
