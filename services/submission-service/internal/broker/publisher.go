package broker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	// ErrNacked means the broker refused responsibility for the message.
	ErrNacked = errors.New("broker nacked the message")
	// ErrUnroutable means no queue was bound for the message (e.g. jobs.queue is missing), so it was dropped.
	ErrUnroutable = errors.New("message was unroutable")
)

// Publisher publishes with publisher confirms: Publish returns nil only once RabbitMQ has taken
// responsibility for the message (for a quorum queue: replicated to a majority). That is what lets the
// outbox mark a row PUBLISHED safely.
//
// Publishes are serialised on one confirm-mode channel; the outbox relay publishes one at a time anyway.
type Publisher struct {
	conn           *Conn
	confirmTimeout time.Duration

	mu      sync.Mutex
	ch      *amqp.Channel
	returns chan amqp.Return
}

func NewPublisher(conn *Conn, confirmTimeout time.Duration) *Publisher {
	return &Publisher{conn: conn, confirmTimeout: confirmTimeout}
}

// Ready reports whether a publish could currently succeed (the connection is up).
func (p *Publisher) Ready() bool {
	_, err := p.conn.Current()
	return err == nil
}

// channel returns the confirm-mode channel, opening a new one if the old one closed. Callers hold p.mu.
func (p *Publisher) channel() (*amqp.Channel, error) {
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	conn, err := p.conn.Current()
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close() //nolint:errcheck
		return nil, err
	}
	// The broker sends a mandatory message back before acking it; the buffer keeps the client's dispatcher from
	// blocking on us. Publishes are serialised, so at most one return is outstanding.
	p.returns = ch.NotifyReturn(make(chan amqp.Return, 16))
	p.ch = ch
	return ch, nil
}

// drop discards the channel after a failure whose outcome is unknown, so the next publish starts clean.
func (p *Publisher) drop() {
	if p.ch != nil {
		p.ch.Close() //nolint:errcheck
		p.ch = nil
	}
}

// Publish sends msg as persistent and mandatory and waits for the broker's confirm.
func (p *Publisher) Publish(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch, err := p.channel()
	if err != nil {
		return err
	}
	msg.DeliveryMode = amqp.Persistent
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, msg)
	if err != nil {
		p.drop()
		return fmt.Errorf("publish: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, p.confirmTimeout)
	defer cancel()
	acked, err := dc.WaitContext(waitCtx)
	if err != nil {
		// Timed out or the channel died: the message may or may not have arrived. Report failure; the outbox
		// retries, and consumers are idempotent if it did arrive.
		p.drop()
		return fmt.Errorf("waiting for confirm: %w", err)
	}
	if !acked {
		return ErrNacked
	}
	select {
	case ret := <-p.returns:
		if ret.MessageId == msg.MessageId {
			return fmt.Errorf("%w: %s (exchange %q, routing key %q)", ErrUnroutable, ret.ReplyText, exchange, routingKey)
		}
	default:
	}
	return nil
}
