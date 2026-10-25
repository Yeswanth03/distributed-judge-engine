// Package broker wraps the RabbitMQ client: a self-healing connection, a confirming publisher, and the
// topology the Submission Service relies on.
package broker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var ErrNotConnected = errors.New("broker not connected")

// SetupFunc runs on every new connection, before anyone else can use it (e.g. to declare topology).
type SetupFunc func(ch *amqp.Channel) error

// Conn keeps one AMQP connection alive. amqp091-go does not reconnect by itself, so Run dials with
// exponential backoff, waits for the connection to drop, and dials again. Publishers and consumers ask for the
// current connection and simply retry when there is none.
type Conn struct {
	url   string
	name  string
	log   *slog.Logger
	setup []SetupFunc

	mu      sync.RWMutex
	conn    *amqp.Connection
	changed chan struct{} // closed and replaced whenever conn changes
}

func NewConn(url string, log *slog.Logger, setup ...SetupFunc) *Conn {
	host, _ := os.Hostname()
	return &Conn{url: url, name: "submission-service@" + host, log: log, setup: setup, changed: make(chan struct{})}
}

func (c *Conn) state() (*amqp.Connection, <-chan struct{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn, c.changed
}

func (c *Conn) set(conn *amqp.Connection) {
	c.mu.Lock()
	c.conn = conn
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}

// Current returns the live connection, or ErrNotConnected.
func (c *Conn) Current() (*amqp.Connection, error) {
	conn, _ := c.state()
	if conn == nil || conn.IsClosed() {
		return nil, ErrNotConnected
	}
	return conn, nil
}

// WaitConnected blocks until there is a live connection or ctx ends.
func (c *Conn) WaitConnected(ctx context.Context) (*amqp.Connection, error) {
	for {
		conn, changed := c.state()
		if conn != nil && !conn.IsClosed() {
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

// Check is the readiness check: an error unless connected.
func (c *Conn) Check(context.Context) error {
	_, err := c.Current()
	return err
}

// Run keeps the connection up until ctx is cancelled, then closes it.
func (c *Conn) Run(ctx context.Context) {
	const minBackoff, maxBackoff = 500 * time.Millisecond, 5 * time.Second
	backoff := minBackoff
	for {
		conn, err := c.dial()
		if err != nil {
			c.log.Warn("broker not reachable, retrying", "error", err, "retry_in", backoff.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = minBackoff
		closed := conn.NotifyClose(make(chan *amqp.Error, 1))
		c.set(conn)
		c.log.Info("broker connected")

		select {
		case <-ctx.Done():
			c.set(nil)
			conn.Close() //nolint:errcheck
			return
		case err := <-closed:
			c.set(nil)
			c.log.Warn("broker connection lost", "error", err)
		}
	}
}

func (c *Conn) dial() (*amqp.Connection, error) {
	conn, err := amqp.DialConfig(c.url, amqp.Config{
		Heartbeat:  10 * time.Second,
		Properties: amqp.Table{"connection_name": c.name}, // shown in the RabbitMQ management UI
	})
	if err != nil {
		return nil, err
	}
	if len(c.setup) > 0 {
		ch, err := conn.Channel()
		if err != nil {
			conn.Close() //nolint:errcheck
			return nil, err
		}
		for _, f := range c.setup {
			if err := f(ch); err != nil {
				conn.Close() //nolint:errcheck
				return nil, fmt.Errorf("connection setup: %w", err)
			}
		}
		ch.Close() //nolint:errcheck
	}
	return conn, nil
}
