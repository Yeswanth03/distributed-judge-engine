package sse

import (
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

func event(id uuid.UUID, st model.Status) model.StatusEvent {
	return model.StatusEvent{SubmissionID: id, Status: st}
}

func TestPublishReachesOnlyThatSubmission(t *testing.T) {
	h := NewHub()
	a, b := uuid.New(), uuid.New()
	sa1, sa2, sb := h.Subscribe(a), h.Subscribe(a), h.Subscribe(b)

	h.Publish(event(a, model.StatusRunning))
	for _, s := range []*Subscriber{sa1, sa2} {
		select {
		case m := <-s.C:
			if m.Event == nil || m.Event.Status != model.StatusRunning {
				t.Fatalf("unexpected message %+v", m)
			}
		default:
			t.Fatal("subscriber of a got nothing")
		}
	}
	select {
	case m := <-sb.C:
		t.Fatalf("subscriber of b got %+v", m)
	default:
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	h := NewHub()
	id := uuid.New()
	s := h.Subscribe(id)
	h.Unsubscribe(s)
	h.Unsubscribe(s)
	if h.Count() != 0 {
		t.Fatalf("count = %d", h.Count())
	}
	h.Publish(event(id, model.StatusRunning)) // nobody listening: must not panic or block
}

func TestPublishNeverBlocksOnAStalledStream(t *testing.T) {
	h := NewHub()
	id := uuid.New()
	h.Subscribe(id) // never reads
	for range bufferSize * 3 {
		h.Publish(event(id, model.StatusRunning)) // would deadlock if Publish blocked
	}
}

func TestResyncReachesEveryStream(t *testing.T) {
	h := NewHub()
	s1, s2 := h.Subscribe(uuid.New()), h.Subscribe(uuid.New())
	h.Resync()
	for _, s := range []*Subscriber{s1, s2} {
		if m := <-s.C; !m.Resync {
			t.Fatalf("got %+v, want resync", m)
		}
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	h := NewHub()
	h.Close()
	h.Close()
	select {
	case <-h.Done():
	default:
		t.Fatal("Done not closed")
	}
}

func TestConcurrentUse(t *testing.T) {
	h := NewHub()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s := h.Subscribe(ids[i%3])
			for range 5 {
				select {
				case <-s.C:
				default:
				}
			}
			h.Unsubscribe(s)
		}()
		go func() {
			defer wg.Done()
			h.Publish(event(ids[i%3], model.StatusRunning))
			if i%10 == 0 {
				h.Resync()
			}
		}()
	}
	wg.Wait()
	if h.Count() != 0 {
		t.Fatalf("count = %d after everyone left", h.Count())
	}
}
