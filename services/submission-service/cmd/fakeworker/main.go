// Command fakeworker stands in for the Judge Worker during development and demos. It follows the worker's
// side of docs/contracts.md exactly, without running any code:
//
//  1. take a job from jobs.queue (manual ack, prefetch 1)
//  2. publish RUNNING to verdict.fanout
//  3. "judge" for a moment; the verdict is chosen from the code so each outcome can be demoed
//  4. publish the verdict (with publisher confirms), and only then ack the job
//
// Verdicts: code containing "while True" → TIME_LIMIT_EXCEEDED, "MemoryError" → MEMORY_LIMIT_EXCEEDED,
// "raise" → RUNTIME_ERROR, "wrong" → WRONG_ANSWER, anything else → ACCEPTED.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

const totalTests = 55

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("component", "fake-worker")
	url := getenv("AMQP_URL", "amqp://judge:judge@localhost:5672/")
	delay, _ := strconv.Atoi(getenv("FAKE_JUDGE_MS", "1500")) // how long "judging" takes

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn := broker.NewConn(url, log, broker.DeclareTopology)
	connCtx, stopConn := context.WithCancel(context.Background())
	connDone := make(chan struct{})
	go func() { conn.Run(connCtx); close(connDone) }()
	pub := broker.NewPublisher(conn, 10*time.Second)

	for ctx.Err() == nil {
		c, err := conn.WaitConnected(ctx)
		if err != nil {
			break
		}
		if err := consume(ctx, c, pub, time.Duration(delay)*time.Millisecond, log); err != nil && ctx.Err() == nil {
			log.Warn("consumer stopped, resubscribing", "error", err)
			time.Sleep(time.Second)
		}
	}
	stopConn()
	<-connDone
	log.Info("stopped")
}

func consume(ctx context.Context, conn *amqp.Connection, pub *broker.Publisher, delay time.Duration, log *slog.Logger) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close() //nolint:errcheck
	if err := ch.Qos(1, 0, false); err != nil {
		return err
	}
	jobs, err := ch.Consume(broker.JobsQueue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	log.Info("waiting for jobs", "queue", broker.JobsQueue)
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-jobs:
			if !ok {
				return nil
			}
			handle(ctx, d, pub, delay, log)
		}
	}
}

func handle(ctx context.Context, d amqp.Delivery, pub *broker.Publisher, delay time.Duration, log *slog.Logger) {
	var job model.JobMessage
	if err := json.Unmarshal(d.Body, &job); err != nil {
		log.Warn("rejecting malformed job", "error", err)
		d.Nack(false, false) //nolint:errcheck // to jobs.dlq
		return
	}
	running := model.StatusEvent{SubmissionID: job.SubmissionID, UserID: job.UserID, ProblemID: job.ProblemID,
		Status: model.StatusRunning}
	if err := publish(ctx, pub, running); err != nil {
		log.Warn("publishing RUNNING failed; job goes back to the queue", "submission_id", job.SubmissionID, "error", err)
		d.Nack(false, true) //nolint:errcheck
		return
	}

	select {
	case <-ctx.Done():
		d.Nack(false, true) //nolint:errcheck // shutting down mid-job: let another worker take it
		return
	case <-time.After(delay):
	}

	verdict := Judge(job)
	if err := publish(ctx, pub, verdict); err != nil {
		log.Warn("publishing verdict failed; job goes back to the queue", "submission_id", job.SubmissionID, "error", err)
		d.Nack(false, true) //nolint:errcheck
		return
	}
	d.Ack(false) //nolint:errcheck // only now: the verdict is safely with the broker
	log.Info("judged", "submission_id", job.SubmissionID, "problem_id", job.ProblemID, "verdict", *verdict.Verdict)
}

// Judge picks a verdict from the code's content, with plausible numbers.
func Judge(job model.JobMessage) model.StatusEvent {
	ev := model.StatusEvent{SubmissionID: job.SubmissionID, UserID: job.UserID, ProblemID: job.ProblemID,
		Status: model.StatusCompleted, TotalTestCases: totalTests,
		ExecutionTimeMS: 20 + rand.IntN(80), MemoryKB: 14000 + rand.IntN(2000)}
	v, msg := model.VerdictAccepted, ""
	failedAt := 1 + rand.IntN(totalTests-1)
	switch {
	case strings.Contains(job.Code, "while True"):
		v, ev.ExecutionTimeMS = model.VerdictTimeLimitExceeded, 2000
	case strings.Contains(job.Code, "MemoryError"):
		v, ev.MemoryKB = model.VerdictMemoryLimitExceeded, 262144
	case strings.Contains(job.Code, "raise"):
		v, msg = model.VerdictRuntimeError, "ValueError: something went wrong (line 3)"
	case strings.Contains(job.Code, "wrong"):
		v = model.VerdictWrongAnswer
	default:
		failedAt = totalTests + 1
	}
	ev.Verdict = &v
	ev.TestCasesPassed = failedAt - 1
	if msg != "" {
		ev.ErrorMessage = &msg
	}
	return ev
}

func publish(ctx context.Context, pub *broker.Publisher, ev model.StatusEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return pub.Publish(ctx, broker.VerdictExchange, "", amqp.Publishing{
		ContentType: "application/json", MessageId: ev.SubmissionID.String(), Timestamp: time.Now(), Body: body,
	})
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
