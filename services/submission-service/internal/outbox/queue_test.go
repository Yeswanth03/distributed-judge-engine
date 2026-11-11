package outbox

import (
	"testing"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/broker"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

func TestQueueFor(t *testing.T) {
	if got := QueueFor(model.EventSubmissionCreated); got != broker.JobsQueue {
		t.Fatalf("submissions go to %s, want %s", got, broker.JobsQueue)
	}
	if got := QueueFor(model.EventRunRequested); got != broker.RunsQueue {
		t.Fatalf("runs go to %s, want %s", got, broker.RunsQueue)
	}
}
