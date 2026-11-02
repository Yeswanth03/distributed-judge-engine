package broker

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// Names and arguments must match deploy/rabbitmq/definitions.json exactly: RabbitMQ rejects a declaration
// whose arguments differ from the existing queue's with PRECONDITION_FAILED (and closes the channel).
const (
	JobsQueue          = "jobs.queue"
	JobsDLQ            = "jobs.dlq"
	VerdictExchange    = "verdicts"
	DeadLetterExchange = "dead-letter"
	VerdictQueue       = "submission-service.verdict.queue"
	VerdictDLQ         = "submission-service.verdict.dlq"

	// VerdictBindingKey receives everything on the exchange: real verdicts and status events alike.
	VerdictBindingKey = "#"
	// StatusRoutingKey is for events Catalog must not receive (it binds verdict.# and dead-letters anything
	// that isn't a complete verdict): reaper give-ups and the fake worker's made-up results.
	StatusRoutingKey = "status.update"
)

func quorum(deliveryLimit int64, dlq string) amqp.Table {
	t := amqp.Table{"x-queue-type": "quorum"}
	if dlq != "" {
		// int64 is sent as an AMQP long, the same type the definitions file import produces.
		t["x-delivery-limit"] = deliveryLimit
		t["x-dead-letter-exchange"] = DeadLetterExchange
		t["x-dead-letter-routing-key"] = dlq
	}
	return t
}

// DeclareTopology declares (idempotently) everything this service publishes to or consumes from. The
// definitions file normally creates it first; declaring again here means the service also works against a
// broker that was set up without that file, such as a fresh one on OpenShift.
func DeclareTopology(ch *amqp.Channel) error {
	for _, ex := range []struct{ name, kind string }{
		{DeadLetterExchange, amqp.ExchangeDirect},
		{VerdictExchange, amqp.ExchangeTopic},
	} {
		if err := ch.ExchangeDeclare(ex.name, ex.kind, true, false, false, false, nil); err != nil {
			return err
		}
	}
	for _, q := range []struct {
		name string
		args amqp.Table
	}{
		{JobsDLQ, quorum(0, "")},
		{JobsQueue, quorum(3, JobsDLQ)},
		{VerdictDLQ, quorum(0, "")},
		{VerdictQueue, quorum(5, VerdictDLQ)},
	} {
		if _, err := ch.QueueDeclare(q.name, true, false, false, false, q.args); err != nil {
			return err
		}
	}
	for _, b := range []struct{ queue, key, exchange string }{
		{JobsDLQ, JobsDLQ, DeadLetterExchange},
		{VerdictDLQ, VerdictDLQ, DeadLetterExchange},
		{VerdictQueue, VerdictBindingKey, VerdictExchange},
	} {
		if err := ch.QueueBind(b.queue, b.key, b.exchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}
