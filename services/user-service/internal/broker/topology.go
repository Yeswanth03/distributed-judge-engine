package broker

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// UsersExchange carries every user event, routed by its type (user.registered, user.updated, ...). It is a
// topic exchange so each consumer binds only the events it needs (docs/contracts.md section 7). Must match
// deploy/rabbitmq/definitions.json.
const UsersExchange = "users"

// DeclareTopology declares (idempotently) the exchange this service publishes to. Queues belong to the
// consumers, which declare and bind their own.
func DeclareTopology(ch *amqp.Channel) error {
	return ch.ExchangeDeclare(UsersExchange, amqp.ExchangeTopic, true, false, false, false, nil)
}
