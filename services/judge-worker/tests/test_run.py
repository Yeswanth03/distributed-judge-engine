"""Trial runs with fake Catalog/Spawner clients: which cases run, what each
case reports, and that hidden cases are never executed."""

import json

from app.models import RunJob
from app.run import MAX_TEXT_CHARS, run_code

PROBLEM_UUID = "11111111-1111-1111-1111-111111111111"
SAMPLE_1 = '{"nums":[2,7,11,15],"target":9}'
SAMPLE_2 = '{"nums":[3,2,4],"target":6}'
HIDDEN = '{"nums":[1,5],"target":6}'


class FakeCatalog:
    def __init__(self, function_name="twoSum"):
        self.function_name = function_name

    def get_testset(self, problem_id):
        assert problem_id == PROBLEM_UUID
        return {
            "problem_id": problem_id,
            "version": 1,
            "content_hash": "abc123",
            "time_limit_ms": 1000,
            "memory_limit_mb": 256,
            "function_name": self.function_name,
            "parameters": [{"name": "nums", "type": "int[]"}, {"name": "target", "type": "int"}],
            "return_type": "int[]" if self.function_name else None,
            "cases": [
                {"ordinal": 1, "input": SAMPLE_1, "expected_output": "[0,1]", "is_sample": True},
                {"ordinal": 2, "input": SAMPLE_2, "expected_output": "[1,2]", "is_sample": True},
                {"ordinal": 3, "input": HIDDEN, "expected_output": "[0,1]", "is_sample": False},
            ],
        }


class FakeSpawner:
    def __init__(self, outputs):
        self._outputs = outputs
        self.stdins = []

    def execute(self, **kwargs):
        self.stdins.append(kwargs["stdin"])
        return self._outputs[len(self.stdins) - 1]


def _exec(result=None, stdout="", stderr="", exit_code=0, timed_out=False, wall_time_ms=12):
    return {
        "stdout": stdout,
        "stderr": stderr,
        "result": result,
        "exit_code": exit_code,
        "wall_time_ms": wall_time_ms,
        "peak_memory_kb": 2048,
        "timed_out": timed_out,
        "oom_killed": False,
    }


def _job(inputs=None):
    return RunJob(
        run_id="run-1",
        user_id="user-1",
        problem_id=PROBLEM_UUID,
        language="python",
        code="class Solution: ...",
        inputs=inputs,
        requested_at="2026-11-11T00:00:00Z",
    )


def test_runs_every_sample_and_never_a_hidden_case():
    spawner = FakeSpawner([_exec(result="[0,1]"), _exec(result="[0,2]")])
    result = run_code(_job(), FakeCatalog(), spawner)
    assert spawner.stdins == [SAMPLE_1, SAMPLE_2]  # both samples ran, even after a failure; no hidden case
    assert [c.verdict for c in result.cases] == ["ACCEPTED", "WRONG_ANSWER"]
    assert result.verdict == "WRONG_ANSWER"
    assert result.cases[1].expected_output == "[1,2]"
    assert result.cases[1].output == "[0,2]"
    assert all(c.kind == "sample" for c in result.cases)
    assert HIDDEN not in json.dumps(result.model_dump())


def test_accepted_when_all_samples_pass():
    spawner = FakeSpawner([_exec(result="[0,1]"), _exec(result="[1,2]")])
    result = run_code(_job(), FakeCatalog(), spawner)
    assert result.verdict == "ACCEPTED"
    assert result.status == "COMPLETED"


def test_prints_are_shown_apart_from_the_answer():
    spawner = FakeSpawner([_exec(result="[0,1]", stdout="checking 2\n"), _exec(result="[1,2]")])
    case = run_code(_job(), FakeCatalog(), spawner).cases[0]
    assert case.output == "[0,1]"
    assert case.stdout == "checking 2\n"
    assert case.verdict == "ACCEPTED"


def test_output_uses_the_same_compact_json_as_the_expected_output():
    spawner = FakeSpawner([_exec(result="[0, 2]"), _exec(result="[1, 2]")])
    result = run_code(_job(), FakeCatalog(), spawner)
    assert [c.output for c in result.cases] == ["[0,2]", "[1,2]"]


def test_custom_input_runs_instead_of_the_samples():
    spawner = FakeSpawner([_exec(result="[0,1]")])
    result = run_code(_job(['{"nums":[5,5],"target":10}']), FakeCatalog(), spawner)
    assert len(spawner.stdins) == 1
    case = result.cases[0]
    assert case.kind == "custom"
    assert case.expected_output is None  # no reference solution to compute it
    assert case.verdict == "FINISHED"
    assert case.output == "[0,1]"
    assert result.verdict is None


def test_custom_input_matching_a_sample_gets_its_expected_output():
    # Same values as SAMPLE_2, written with spaces and over two lines.
    spawner = FakeSpawner([_exec(result="[1,2]")])
    result = run_code(_job(['{"nums": [3, 2, 4],\n "target": 6}']), FakeCatalog(), spawner)
    assert spawner.stdins == [SAMPLE_2]  # compacted to the single line the driver reads
    assert result.cases[0].expected_output == "[1,2]"
    assert result.verdict == "ACCEPTED"


def test_invalid_custom_input_is_reported_without_running():
    spawner = FakeSpawner([])
    result = run_code(_job(["not json", "[1, 2]"]), FakeCatalog(), spawner)
    assert spawner.stdins == []
    assert [c.verdict for c in result.cases] == ["INVALID_INPUT", "INVALID_INPUT"]
    assert "JSON" in result.cases[0].error
    assert "object" in result.cases[1].error


def test_runtime_error_shows_the_full_error_output():
    traceback = 'Traceback (most recent call last):\n  ...\nNameError: name "x" is not defined\n'
    spawner = FakeSpawner([_exec(exit_code=1, stderr=traceback), _exec(result="[1,2]")])
    case = run_code(_job(), FakeCatalog(), spawner).cases[0]
    assert case.verdict == "RUNTIME_ERROR"
    assert case.error == traceback
    assert case.output is None


def test_time_limit_is_reported_per_case():
    spawner = FakeSpawner([_exec(exit_code=137, timed_out=True, wall_time_ms=1050), _exec(result="[1,2]")])
    result = run_code(_job(), FakeCatalog(), spawner)
    assert result.cases[0].verdict == "TIME_LIMIT_EXCEEDED"
    assert result.cases[0].error == "Time limit exceeded"
    assert result.verdict == "TIME_LIMIT_EXCEEDED"


def test_long_output_is_cut_off():
    spawner = FakeSpawner([_exec(result="[0,1]", stdout="x" * (MAX_TEXT_CHARS + 500)), _exec(result="[1,2]")])
    case = run_code(_job(), FakeCatalog(), spawner).cases[0]
    assert len(case.stdout) < MAX_TEXT_CHARS + 50
    assert case.stdout.endswith("(cut off)")


def test_legacy_problem_uses_stdout_as_the_output():
    spawner = FakeSpawner([_exec(stdout="[0,1]\n"), _exec(stdout="nope\n")])
    result = run_code(_job(), FakeCatalog(function_name=None), spawner)
    assert [c.verdict for c in result.cases] == ["ACCEPTED", "WRONG_ANSWER"]
    assert result.cases[1].output == "nope\n"
    assert result.cases[1].stdout == ""  # the prints ARE the output here


def test_run_job_matches_the_contract_message():
    job = RunJob.model_validate(
        {
            "run_id": "dc7626b3-f4e7-4194-a625-300911712913",
            "user_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
            "problem_id": PROBLEM_UUID,
            "language": "python",
            "code": "class Solution: ...",
            "inputs": None,
            "requested_at": "2026-11-11T10:00:00Z",
        }
    )
    assert job.inputs is None
