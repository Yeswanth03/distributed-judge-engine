"""Tests the verdict-building logic in isolation, with fake Catalog/Spawner
clients, no network or Docker needed. This is exactly the kind of test the
design doc's testing notes call for: unit-test verdict logic without a real
broker or sandbox."""

from app.judge import judge_submission
from app.models import Job


class FakeCatalog:
    def get_testset(self, problem_id):
        return {
            "problem_id": problem_id,
            "version": 1,
            "content_hash": "abc123",
            "time_limit_ms": 1000,
            "memory_limit_mb": 256,
            "cases": [
                {"ordinal": 1, "input": "2 3\n", "expected_output": "5", "is_sample": True},
                {"ordinal": 2, "input": "10 20\n", "expected_output": "30", "is_sample": False},
            ],
        }


class FakeSpawner:
    def __init__(self, outputs):
        self._outputs = outputs
        self.calls = 0

    def execute(self, **kwargs):
        result = self._outputs[self.calls]
        self.calls += 1
        return result


def _job():
    return Job(
        submission_id="sub-1",
        problem_id="prob-1",
        user_id="user-1",
        language="python3.11",
        source_code="print(5)",
        submitted_at="2026-01-01T00:00:00Z",
    )


def _exec(stdout="", exit_code=0, timed_out=False, oom_killed=False, wall_time_ms=10, peak_memory_kb=1000):
    return {
        "stdout": stdout,
        "stderr": "",
        "exit_code": exit_code,
        "wall_time_ms": wall_time_ms,
        "peak_memory_kb": peak_memory_kb,
        "timed_out": timed_out,
        "oom_killed": oom_killed,
    }


def test_accepted_when_all_cases_pass():
    spawner = FakeSpawner([_exec(stdout="5"), _exec(stdout="30")])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "accepted"
    assert len(verdict.test_results) == 2
    assert all(r.passed for r in verdict.test_results)
    assert verdict.testset_version == 1
    assert verdict.content_hash == "abc123"
    # Required by Catalog's VerdictMessageParser, carried through from the job.
    assert verdict.user_id == "user-1"
    assert verdict.problem_id == "prob-1"
    assert verdict.rejudge_seq == 0


def test_stops_at_first_failing_case():
    spawner = FakeSpawner([_exec(stdout="wrong")])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "wrong_answer"
    assert spawner.calls == 1  # second case never ran


def test_timeout_classified_correctly():
    spawner = FakeSpawner([_exec(exit_code=137, timed_out=True, oom_killed=False)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "time_limit_exceeded"


def test_oom_classified_correctly():
    spawner = FakeSpawner([_exec(exit_code=137, timed_out=False, oom_killed=True)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "memory_limit_exceeded"


def test_runtime_error_when_nonzero_exit():
    spawner = FakeSpawner([_exec(exit_code=1)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "runtime_error"


def test_records_worker_id_and_timing():
    spawner = FakeSpawner([_exec(stdout="5", wall_time_ms=42), _exec(stdout="30", wall_time_ms=58)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-7")
    assert verdict.worker_id == "worker-7"
    assert verdict.total_time_ms == 100
