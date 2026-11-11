"""Tests the verdict-building logic in isolation, with fake Catalog/Spawner
clients, no network or Docker needed. This is exactly the kind of test the
design doc's testing notes call for: unit-test verdict logic without a real
broker or sandbox."""

from app.judge import _values_equal, judge_submission
from app.models import Job


PROBLEM_UUID = "11111111-1111-1111-1111-111111111111"


class FakeCatalog:
    def get_testset(self, problem_id):
        assert problem_id == PROBLEM_UUID
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
        problem_id=PROBLEM_UUID,
        user_id="user-1",
        language="python",
        code="print(5)",
        submitted_at="2026-01-01T00:00:00Z",
    )


def _exec(
    stdout="",
    stderr="",
    result=None,
    exit_code=0,
    timed_out=False,
    oom_killed=False,
    wall_time_ms=10,
    peak_memory_kb=1000,
):
    return {
        "stdout": stdout,
        "stderr": stderr,
        "result": result,
        "exit_code": exit_code,
        "wall_time_ms": wall_time_ms,
        "peak_memory_kb": peak_memory_kb,
        "timed_out": timed_out,
        "oom_killed": oom_killed,
    }


def test_accepted_when_all_cases_pass():
    spawner = FakeSpawner([_exec(stdout="5"), _exec(stdout="30")])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "ACCEPTED"
    assert verdict.status == "COMPLETED"
    assert verdict.test_cases_passed == 2
    assert verdict.total_test_cases == 2
    assert verdict.error_message is None
    assert len(verdict.test_results) == 2
    assert all(r.passed for r in verdict.test_results)
    assert verdict.testset_version == 1
    assert verdict.content_hash == "abc123"
    # Required by Catalog's VerdictMessageParser, which parses problem_id as a UUID.
    assert verdict.user_id == "user-1"
    assert verdict.problem_id == PROBLEM_UUID
    assert verdict.rejudge_seq == 0


def test_stops_at_first_failing_case():
    spawner = FakeSpawner([_exec(stdout="wrong")])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "WRONG_ANSWER"
    assert spawner.calls == 1  # second case never ran
    assert verdict.test_cases_passed == 0
    assert verdict.total_test_cases == 2


def test_timeout_classified_correctly():
    spawner = FakeSpawner([_exec(exit_code=137, timed_out=True, oom_killed=False)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "TIME_LIMIT_EXCEEDED"


def test_oom_classified_correctly():
    spawner = FakeSpawner([_exec(exit_code=137, timed_out=False, oom_killed=True)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "MEMORY_LIMIT_EXCEEDED"


def test_runtime_error_when_nonzero_exit():
    spawner = FakeSpawner([_exec(exit_code=1)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    assert verdict.verdict == "RUNTIME_ERROR"
    assert verdict.error_message == "Program exited with code 1"


def test_legacy_problem_failing_sample_case_uses_stdout_as_actual_output():
    # No function_name on this testset (FakeCatalog), so this is the legacy
    # stdin/stdout path: actual_output must fall back to stdout, there's no
    # driver result file in play at all.
    spawner = FakeSpawner([_exec(stdout="wrong")])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-1")
    failed = verdict.test_results[0]
    assert failed.actual_output == "wrong"
    assert failed.expected_output == "5"


def test_records_worker_id_and_timing():
    spawner = FakeSpawner([_exec(stdout="5", wall_time_ms=42), _exec(stdout="30", wall_time_ms=58)])
    verdict = judge_submission(_job(), FakeCatalog(), spawner, "worker-7")
    assert verdict.worker_id == "worker-7"
    assert verdict.execution_time_ms == 100
    assert verdict.memory_kb == 1000


class FakeCatalogFunctionProblem:
    """A function-based problem (docs/contracts.md section 5): Two Sum, same
    shape Catalog's own sample data now generates since function details
    became mandatory."""

    def get_testset(self, problem_id):
        return {
            "problem_id": problem_id,
            "version": 2,
            "content_hash": "def456",
            "time_limit_ms": 1000,
            "memory_limit_mb": 256,
            "function_name": "twoSum",
            "parameters": [{"name": "nums", "type": "int[]"}, {"name": "target", "type": "int"}],
            "return_type": "int[]",
            "cases": [
                {"ordinal": 1, "input": '{"nums":[3,8,12,5],"target":17}', "expected_output": "[2,3]", "is_sample": True},
                {"ordinal": 2, "input": '{"nums":[4,-1,4],"target":8}', "expected_output": "[0,2]", "is_sample": False},
            ],
        }


def _function_job(code: str = "class Solution:\n    def twoSum(self, nums, target):\n        return [2, 3]\n") -> Job:
    return Job(
        submission_id="sub-2",
        problem_id=PROBLEM_UUID,
        user_id="user-1",
        language="python",
        code=code,
        submitted_at="2026-01-01T00:00:00Z",
    )


def test_function_problem_builds_a_driver_and_passes_on_correct_result():
    # The real Spawner would run the driver and return its result file's
    # content separately from stdout; the fake just returns it directly to
    # isolate judge.py's own logic.
    spawner = FakeSpawner([_exec(result="[2, 3]"), _exec(result="[0, 2]")])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    assert verdict.verdict == "ACCEPTED"
    assert verdict.test_cases_passed == 2


def test_function_problem_compares_parsed_json_not_text():
    # "[2, 3]" and "[2,3]" are different text but the same JSON value: a purely
    # textual compare (the legacy path) would wrongly reject this.
    spawner = FakeSpawner([_exec(result="[2, 3]"), _exec(result="[0, 2]")])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    assert verdict.verdict != "WRONG_ANSWER"


def test_function_problem_rejects_actually_wrong_json_value():
    spawner = FakeSpawner([_exec(result="[9, 9]")])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    assert verdict.verdict == "WRONG_ANSWER"


def test_function_problem_ignores_debug_prints_on_stdout():
    # stdout has debug noise on it; the result file is what's actually compared,
    # so the noise never has a chance to interfere.
    spawner = FakeSpawner([_exec(stdout="debugging stuff", result="[2, 3]"), _exec(result="[0, 2]")])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    assert verdict.verdict == "ACCEPTED"


def test_function_problem_rejects_missing_result_as_wrong_answer_not_a_crash():
    # exit_code 0 but no result file (shouldn't normally happen, but must not
    # crash the worker if it does) is treated as a wrong answer, not a 500.
    spawner = FakeSpawner([_exec(result=None, exit_code=0)])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    assert verdict.verdict == "WRONG_ANSWER"


def test_failing_sample_case_includes_diagnostics():
    spawner = FakeSpawner([_exec(result="[9, 9]", wall_time_ms=15, peak_memory_kb=2000)])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    failed = verdict.test_results[0]
    assert failed.test_case_ordinal == 1  # the sample case, per FakeCatalogFunctionProblem
    assert failed.input == '{"nums":[3,8,12,5],"target":17}'
    assert failed.expected_output == "[2,3]"
    assert failed.actual_output == "[9, 9]"
    assert failed.error is None  # wrong answer, nothing crashed


def test_failing_sample_case_includes_the_crash_reason_as_its_error():
    spawner = FakeSpawner([_exec(exit_code=1, stderr="ZeroDivisionError: division by zero")])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    failed = verdict.test_results[0]
    assert failed.error == "ZeroDivisionError: division by zero"


def test_failing_hidden_case_never_includes_diagnostics():
    # Case 2 in FakeCatalogFunctionProblem is hidden (is_sample: False). Make
    # case 1 pass so judging reaches case 2, then fail there.
    spawner = FakeSpawner([_exec(result="[2, 3]"), _exec(result="[9, 9]")])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    failed = verdict.test_results[1]
    assert failed.test_case_ordinal == 2
    assert failed.passed is False
    assert failed.input is None
    assert failed.expected_output is None
    assert failed.actual_output is None
    assert failed.error is None


def test_passing_sample_case_never_includes_diagnostics():
    # Nothing to report for a case that passed, sample or not.
    spawner = FakeSpawner([_exec(result="[2, 3]"), _exec(result="[0, 2]")])
    verdict = judge_submission(_function_job(), FakeCatalogFunctionProblem(), spawner, "worker-1")
    passed_sample = verdict.test_results[0]
    assert passed_sample.passed is True
    assert passed_sample.input is None
    assert passed_sample.actual_output is None


def test_values_equal_applies_float_tolerance():
    assert _values_equal(1.0000001, 1.0, "float") is True
    assert _values_equal(1.5, 1.0, "float") is False


def test_values_equal_checks_array_element_types_recursively():
    assert _values_equal([[1, 2], [3, 4]], [[1, 2], [3, 4]], "int[][]") is True
    assert _values_equal([[1, 2]], [[1, 3]], "int[][]") is False


def test_values_equal_handles_bool():
    assert _values_equal(True, True, "bool") is True
    assert _values_equal(False, True, "bool") is False


def test_values_equal_handles_string():
    assert _values_equal("hello", "hello", "string") is True
    assert _values_equal("hello", "world", "string") is False


def test_job_matches_the_contract_message():
    job = Job.model_validate(
        {
            "submission_id": "dc7626b3-f4e7-4194-a625-300911712913",
            "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
            "problem_id": "11111111-1111-1111-1111-111111111111",
            "language": "python",
            "code": "print(5)",
            "submitted_at": "2026-10-25T11:00:00Z",
        }
    )
    assert job.code == "print(5)"
