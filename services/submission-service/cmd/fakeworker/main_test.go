package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/consumer"
	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

func TestJudgePicksVerdictFromCode(t *testing.T) {
	cases := map[string]model.Verdict{
		"class Solution: pass":            model.VerdictAccepted,
		"while True: pass":                model.VerdictTimeLimitExceeded,
		"x = [0] * 10**10  # MemoryError": model.VerdictMemoryLimitExceeded,
		"raise ValueError()":              model.VerdictRuntimeError,
		"return wrong":                    model.VerdictWrongAnswer,
	}
	for code, want := range cases {
		ev := Judge(model.JobMessage{SubmissionID: uuid.New(), Code: code})
		if *ev.Verdict != want {
			t.Errorf("%q: verdict %s, want %s", code, *ev.Verdict, want)
		}
		accepted := want == model.VerdictAccepted
		if accepted != (ev.TestCasesPassed == ev.TotalTestCases) {
			t.Errorf("%q: passed %d of %d", code, ev.TestCasesPassed, ev.TotalTestCases)
		}
		// Whatever the fake worker emits must be accepted by the real consumer.
		body, _ := json.Marshal(ev)
		if _, err := consumer.ParseEvent(body); err != nil {
			t.Errorf("%q: consumer rejects the event: %v", code, err)
		}
	}
}
