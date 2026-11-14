"""Tests harness.build_driver in isolation: given a submission and a function
name, does the driver actually import the submission, call the function, and
write the right JSON to its own result file? Runs both files together with
the real Python interpreter in a temp directory (no Docker needed, this only
checks the driver's own correctness, not sandboxing), mirroring exactly how
Spawner's docker_runner.py lays them out: solution.py and driver.py side by
side, driver.py is what actually runs."""

import json
import subprocess
import sys
import tempfile
from pathlib import Path

from app.harness import build_driver


def _run(submission: str, function_name: str, stdin: str):
    driver = build_driver(function_name)
    with tempfile.TemporaryDirectory() as tmp:
        tmp_path = Path(tmp)
        (tmp_path / "solution.py").write_text(submission)
        (tmp_path / "driver.py").write_text(driver)
        result = subprocess.run(
            [sys.executable, "driver.py"],
            input=stdin,
            capture_output=True,
            text=True,
            cwd=tmp,
            timeout=5,
        )
        result_path = tmp_path / "result.json"
        result_json = result_path.read_text() if result_path.exists() else None
        return result, result_json


def test_driver_calls_the_function_and_writes_the_result_file():
    submission = "class Solution:\n    def twoSum(self, nums, target):\n        return [0, 1]\n"
    result, result_json = _run(submission, "twoSum", '{"nums": [2, 7, 11, 15], "target": 9}\n')
    assert result.returncode == 0, result.stderr
    assert json.loads(result_json) == [0, 1]


def test_driver_passes_arguments_by_name_not_position():
    # If the driver didn't use **kwargs correctly, swapping argument order in
    # the JSON object would still have to produce the right call.
    submission = "class Solution:\n    def divide(self, a, b):\n        return a - b\n"
    result, result_json = _run(submission, "divide", '{"b": 3, "a": 10}\n')
    assert result.returncode == 0, result.stderr
    assert json.loads(result_json) == 7


def test_submission_debug_prints_never_touch_the_result_file():
    """The whole point of a separate result file: print() debugging lands on
    stdout, the result file is untouched by it either way."""
    submission = (
        "class Solution:\n"
        "    def twoSum(self, nums, target):\n"
        "        print('debugging:', nums)\n"
        "        return [0, 1]\n"
    )
    result, result_json = _run(submission, "twoSum", '{"nums": [2, 7], "target": 9}\n')
    assert result.returncode == 0, result.stderr
    assert json.loads(result_json) == [0, 1]
    assert "debugging: [2, 7]" in result.stdout
    # The debug print is on stdout, not in the result file, not even alongside it.
    assert "debugging" not in result_json


def test_driver_writes_bool_as_lowercase_json_not_python_capitalized():
    # Catalog sends expected_output as JSON ("true"/"false"), not Python's
    # True/False, so json.dump must be what actually writes the file,
    # confirmed against the real seeded valid-parentheses problem's
    # return_type "bool".
    submission = "class Solution:\n    def isValid(self, s):\n        return len(s) % 2 == 0\n"
    result, result_json = _run(submission, "isValid", '{"s": "()"}\n')
    assert result.returncode == 0, result.stderr
    assert result_json == "true"


def test_submission_missing_the_declared_function_fails_loudly_not_silently():
    """A real edge case, not hypothetical: verified live against the real
    Submission Service, a submission with the wrong method name completed as
    RUNTIME_ERROR, not a hang. This pins that behavior at the driver level:
    calling a method that doesn't exist on Solution must raise (non-zero
    exit, no result file), never exit 0 with nothing written."""
    submission = "class Solution:\n    def wrongMethodName(self, nums, target):\n        return [0, 1]\n"
    result, result_json = _run(submission, "twoSum", '{"nums": [2, 7], "target": 9}\n')
    assert result.returncode != 0
    assert "AttributeError" in result.stderr
    assert result_json is None
