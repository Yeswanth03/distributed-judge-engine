package broker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Tests run against a real RabbitMQ with deploy/rabbitmq/definitions.json loaded: TEST_AMQP_URL, or the
// docker-compose one by default. As with the database tests, an explicit TEST_AMQP_URL that is unreachable
// fails instead of skipping.
const defaultTestAMQPURL = "amqp://judge:judge@localhost:5672/"

func testURL(t *testing.T) string {
	t.Helper()
	u, explicit := os.LookupEnv("TEST_AMQP_URL")
	if !explicit {
		u = defaultTestAMQPURL
	}
	conn, err := amqp.Dial(u)
	if err != nil {
		if explicit {
			t.Fatalf("TEST_AMQP_URL not reachable: %v", err)
		}
		t.Skipf("RabbitMQ not reachable (%v); run `docker compose up -d rabbitmq` or set TEST_AMQP_URL", err)
	}
	conn.Close()
	return u
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// startConn runs a Conn until the test ends and waits for its first connection.
func startConn(t *testing.T, setup ...SetupFunc) *Conn {
	t.Helper()
	c := NewConn(testURL(t), quietLog(), setup...)
	c.name = "submission-service-test-" + uuid.NewString()[:8]
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelWait()
	if _, err := c.WaitConnected(waitCtx); err != nil {
		t.Fatalf("never connected: %v", err)
	}
	return c
}

// tempQueue declares an exclusive queue on its own connection and returns a function that fetches one message.
func tempQueue(t *testing.T) (name string, get func() (amqp.Delivery, bool)) {
	t.Helper()
	conn, err := amqp.Dial(testURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return q.Name, func() (amqp.Delivery, bool) {
		for range 50 {
			d, ok, err := ch.Get(q.Name, true)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				return d, true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return amqp.Delivery{}, false
	}
}

func TestDeclareTopologyMatchesDefinitionsFile(t *testing.T) {
	// If an argument differed from deploy/rabbitmq/definitions.json (name, value or AMQP type), the broker would
	// answer PRECONDITION_FAILED and setup would fail, so the connection would never come up.
	startConn(t, DeclareTopology)
}

func TestPublishIsConfirmed(t *testing.T) {
	c := startConn(t)
	queue, get := tempQueue(t)
	p := NewPublisher(c, 5*time.Second)

	id := uuid.NewString()
	err := p.Publish(context.Background(), "", queue, amqp.Publishing{
		ContentType: "application/json", MessageId: id, Body: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := get()
	if !ok {
		t.Fatal("message never arrived")
	}
	if d.MessageId != id || d.DeliveryMode != amqp.Persistent || string(d.Body) != `{"ok":true}` {
		t.Fatalf("unexpected delivery %+v", d)
	}
}

func TestPublishUnroutableIsAnError(t *testing.T) {
	c := startConn(t)
	p := NewPublisher(c, 5*time.Second)
	err := p.Publish(context.Background(), "", "no-such-queue-"+uuid.NewString(), amqp.Publishing{MessageId: uuid.NewString()})
	if !errors.Is(err, ErrUnroutable) {
		t.Fatalf("err = %v, want ErrUnroutable", err)
	}
	// The channel is still usable afterwards.
	queue, get := tempQueue(t)
	if err := p.Publish(context.Background(), "", queue, amqp.Publishing{MessageId: "next"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := get(); !ok {
		t.Fatal("publish after an unroutable one never arrived")
	}
}

func TestPublishWithoutConnection(t *testing.T) {
	c := NewConn("amqp://judge:judge@127.0.0.1:1/", quietLog()) // never Run
	p := NewPublisher(c, time.Second)
	if p.Ready() {
		t.Fatal("must not be ready without a connection")
	}
	if err := p.Publish(context.Background(), "", "x", amqp.Publishing{}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

// managementURL derives the management API address from the AMQP URL (same host and credentials, port 15672).
func managementURL(t *testing.T) *url.URL {
	u, err := url.Parse(testURL(t))
	if err != nil {
		t.Fatal(err)
	}
	return &url.URL{Scheme: "http", User: u.User, Host: u.Hostname() + ":15672", Path: "/api/"}
}

// killConnection makes the broker close our connection, as a broker restart or network failure would.
func killConnection(t *testing.T, connectionName string) {
	t.Helper()
	api := managementURL(t)
	deadline := time.Now().Add(15 * time.Second) // the management API lists new connections with a delay
	for {
		resp, err := http.Get(api.JoinPath("connections").String())
		if err != nil {
			t.Skipf("management API not reachable: %v", err)
		}
		var conns []struct {
			Name             string `json:"name"`
			ClientProperties struct {
				ConnectionName string `json:"connection_name"`
			} `json:"client_properties"`
		}
		json.NewDecoder(resp.Body).Decode(&conns) //nolint:errcheck
		resp.Body.Close()
		for _, c := range conns {
			if c.ClientProperties.ConnectionName != connectionName {
				continue
			}
			req, _ := http.NewRequest(http.MethodDelete, api.JoinPath("connections", c.Name).String(), nil)
			req.Header.Set("X-Reason", "test: simulated broker failure")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("closing connection: HTTP %d", resp.StatusCode)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("connection %q never showed up in the management API", connectionName)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestReconnectsAfterBrokerClosesConnection(t *testing.T) {
	setups := 0
	c := startConn(t, func(*amqp.Channel) error { setups++; return nil })
	p := NewPublisher(c, 5*time.Second)
	queue, get := tempQueue(t)
	first, _ := c.Current()

	if err := p.Publish(context.Background(), "", queue, amqp.Publishing{MessageId: "before"}); err != nil {
		t.Fatal(err)
	}
	killConnection(t, c.name)

	// Wait for a new connection (not the killed one).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		conn, err := c.WaitConnected(ctx)
		if err != nil {
			t.Fatalf("did not reconnect: %v", err)
		}
		if conn != first {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if setups != 2 {
		t.Fatalf("setup ran %d times, want once per connection (2)", setups)
	}

	// The publisher notices its channel died with the old connection and opens a new one.
	if err := p.Publish(context.Background(), "", queue, amqp.Publishing{MessageId: "after"}); err != nil {
		t.Fatalf("publish after reconnect: %v", err)
	}
	for _, want := range []string{"before", "after"} {
		d, ok := get()
		if !ok || d.MessageId != want {
			t.Fatalf("got %q (ok=%v), want %q", d.MessageId, ok, want)
		}
	}
}
