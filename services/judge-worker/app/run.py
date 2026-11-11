"""Trial runs ("Run" in the editor): the submitted code against the problem's
sample cases, or against the user's own inputs. Nothing here is scored, and
hidden test cases are never executed, so a run can show everything: return
values, the code's own prints and the full error output."""

import json
from datetime import datetime, timezone

from .catalog_client import CatalogClient
from .harness import build_driver
from .judge import _actual_output, _classify, _error_text
from .models import RunCase, RunJob, RunResult
from .spawner_client import SpawnerClient

# Long enough for any reasonable debugging output, short enough that a print()
# in a loop can't bloat the result message.
MAX_TEXT_CHARS = 10_000


def run_code(job: RunJob, catalog: CatalogClient, spawner: SpawnerClient) -> RunResult:
    testset = catalog.get_testset(job.problem_id)
    function_name = testset.get("function_name")
    return_type = testset.get("return_type")
    driver_code = build_driver(function_name) if function_name else None
    samples = [c for c in testset["cases"] if c.get("is_sample")]

    if job.inputs:
        planned = [("custom", text, _expected_for(text, samples, function_name)) for text in job.inputs]
    else:
        planned = [("sample", c["input"], c["expected_output"]) for c in samples]

    cases = []
    for kind, text, expected in planned:
        stdin, problem = _prepare_input(text, function_name)
        if problem:
            cases.append(RunCase(kind=kind, input=text, expected_output=expected, error=problem, verdict="INVALID_INPUT"))
            continue
        exec_result = spawner.execute(
            submission_id=job.run_id,
            language=job.language,
            source_code=job.code,
            driver_code=driver_code,
            stdin=stdin,
            time_limit_ms=testset["time_limit_ms"],
            memory_limit_mb=testset["memory_limit_mb"],
        )
        cases.append(_case(kind, text, expected, exec_result, return_type))

    judged = [c.verdict for c in cases if c.expected_output is not None and c.verdict != "INVALID_INPUT"]
    verdict = next((v for v in judged if v != "ACCEPTED"), "ACCEPTED") if judged else None
    return RunResult(
        run_id=job.run_id,
        verdict=verdict,
        cases=cases,
        finished_at=datetime.now(timezone.utc).isoformat(),
    )


def _case(kind: str, text: str, expected: str | None, exec_result: dict, return_type: str | None) -> RunCase:
    if expected is not None:
        verdict = _classify(exec_result, expected, return_type)
    else:
        verdict = _classify(exec_result, "", return_type)
        if verdict in ("ACCEPTED", "WRONG_ANSWER"):
            verdict = "FINISHED"  # it ran, but there's nothing to compare against
    crashed = verdict in ("TIME_LIMIT_EXCEEDED", "MEMORY_LIMIT_EXCEEDED", "RUNTIME_ERROR")
    # For a legacy stdin/stdout problem the prints ARE the output.
    stdout = exec_result["stdout"] if return_type is not None else ""
    return RunCase(
        kind=kind,
        input=text,
        expected_output=expected,
        output=None if crashed else _clip(_compact(_actual_output(exec_result, return_type), return_type)),
        stdout=_clip(stdout),
        error=_clip(_error_text(exec_result, verdict)),
        verdict=verdict,
        time_ms=exec_result["wall_time_ms"],
        memory_kb=exec_result.get("peak_memory_kb"),
    )


def _prepare_input(text: str, function_name: str | None) -> tuple[str, str | None]:
    """Returns what to feed the program and, if the input can't work, why.
    The driver reads exactly one line of JSON, so a custom input typed over
    several lines is compacted first."""
    if function_name is None:
        return text, None
    try:
        args = json.loads(text)
    except ValueError:
        return text, 'Input must be JSON with one value per parameter, like {"nums": [2, 7], "target": 9}'
    if not isinstance(args, dict):
        return text, "Input must be a JSON object with one value per parameter, not a single value"
    return json.dumps(args, separators=(",", ":")), None


def _expected_for(text: str, samples: list[dict], function_name: str | None) -> str | None:
    """A custom input that is really one of the samples gets that sample's
    expected output. Any other custom input has none: there is no reference
    solution to compute it."""
    for case in samples:
        if function_name is None:
            if case["input"].strip() == text.strip():
                return case["expected_output"]
            continue
        try:
            if json.loads(case["input"]) == json.loads(text):
                return case["expected_output"]
        except ValueError:
            continue
    return None


def _compact(output: str, return_type: str | None) -> str:
    """Shows a return value in the same compact JSON as the expected output
    ([0,1], not [0, 1]), so the two can be compared at a glance."""
    if return_type is None:
        return output
    try:
        return json.dumps(json.loads(output), separators=(",", ":"))
    except ValueError:
        return output


def _clip(text: str | None) -> str | None:
    if text is None or len(text) <= MAX_TEXT_CHARS:
        return text
    return text[:MAX_TEXT_CHARS] + "\n... (cut off)"
