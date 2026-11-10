import json
from datetime import datetime, timezone

from .catalog_client import CatalogClient
from .harness import build_driver
from .models import Job, TestResult, Verdict
from .spawner_client import SpawnerClient


def judge_submission(job: Job, catalog: CatalogClient, spawner: SpawnerClient, worker_id: str) -> Verdict:
    testset = catalog.get_testset(job.problem_id)
    function_name = testset.get("function_name")
    return_type = testset.get("return_type")

    # function_name is null only for a problem published before function
    # details existed (docs/contracts.md section 5); everything else is a
    # function call, not a stdin/stdout program. The submission itself is
    # never modified either way: the driver is a separate script.
    driver_code = build_driver(function_name) if function_name else None

    results: list[TestResult] = []
    verdict = "ACCEPTED"
    error_message = None
    total_time_ms = 0
    max_memory_kb = 0

    for case in testset["cases"]:
        exec_result = spawner.execute(
            submission_id=job.submission_id,
            language=job.language,
            source_code=job.code,
            driver_code=driver_code,
            stdin=case["input"],
            time_limit_ms=testset["time_limit_ms"],
            memory_limit_mb=testset["memory_limit_mb"],
        )

        case_verdict = _classify(exec_result, case["expected_output"], return_type)
        passed = case_verdict == "ACCEPTED"

        # Diagnostics only for a failing SAMPLE case; never for a hidden one,
        # passing or not, so hidden test data can never leak through a verdict.
        diagnostics = {}
        if not passed and case.get("is_sample"):
            diagnostics = {
                "input": case["input"],
                "expected_output": case["expected_output"],
                "actual_output": _actual_output(exec_result, return_type),
                "error": _error_text(exec_result, case_verdict),
            }

        results.append(
            TestResult(
                test_case_ordinal=case["ordinal"],
                passed=passed,
                time_ms=exec_result["wall_time_ms"],
                memory_kb=exec_result.get("peak_memory_kb"),
                **diagnostics,
            )
        )
        total_time_ms += exec_result["wall_time_ms"]
        max_memory_kb = max(max_memory_kb, exec_result.get("peak_memory_kb") or 0)

        if not passed:
            verdict = case_verdict
            if verdict == "RUNTIME_ERROR":
                # The message, not stderr itself: a submission could print the
                # hidden test input to stderr, and this becomes error_message
                # on the published Verdict regardless of sample or hidden.
                error_message = f"Program exited with code {exec_result['exit_code']}"
            break  # stop at the first failing case, like most judges do

    return Verdict(
        submission_id=job.submission_id,
        user_id=job.user_id,
        problem_id=job.problem_id,
        testset_version=testset["version"],
        verdict=verdict,
        rejudge_seq=0,  # always 0 until re-judging a submission is built
        judged_at=datetime.now(timezone.utc).isoformat(),
        execution_time_ms=total_time_ms,
        memory_kb=max_memory_kb or None,
        test_cases_passed=sum(r.passed for r in results),
        total_test_cases=len(testset["cases"]),
        error_message=error_message,
        test_results=results,
        content_hash=testset["content_hash"],
        worker_id=worker_id,
    )


def _actual_output(exec_result: dict, return_type: str | None) -> str:
    """What to show as the submission's actual output: the driver's own result
    file for a function problem (never stdout, print() debugging could be
    sitting there instead), or plain stdout for a legacy stdin/stdout one."""
    if return_type is not None and exec_result.get("result"):
        return exec_result["result"]
    return exec_result["stdout"]


def _error_text(exec_result: dict, case_verdict: str) -> str | None:
    if case_verdict == "TIME_LIMIT_EXCEEDED":
        return "Time limit exceeded"
    if case_verdict == "MEMORY_LIMIT_EXCEEDED":
        return "Memory limit exceeded"
    if case_verdict == "RUNTIME_ERROR":
        return exec_result.get("stderr") or f"Program exited with code {exec_result['exit_code']}"
    return None  # WRONG_ANSWER: the value mismatch itself is the finding, nothing crashed


def _classify(exec_result: dict, expected_output: str, return_type: str | None) -> str:
    """The verdict values in docs/contracts.md. Catalog's VerdictMessageParser
    lowercases whatever it receives, so this suits both consumers.

    return_type is None only for a legacy, pre-function-details problem: those
    still compare stdout as plain text. Everything else compares the driver's
    own result file as parsed JSON, never text, per the function-call contract.
    """
    if exec_result["timed_out"]:
        return "TIME_LIMIT_EXCEEDED"
    if exec_result["oom_killed"]:
        return "MEMORY_LIMIT_EXCEEDED"
    if exec_result["exit_code"] != 0:
        return "RUNTIME_ERROR"
    if return_type is None:
        matched = exec_result["stdout"].strip() == expected_output.strip()
    else:
        matched = _function_result_matches(exec_result, expected_output, return_type)
    return "ACCEPTED" if matched else "WRONG_ANSWER"


def _function_result_matches(exec_result: dict, expected_output: str, return_type: str) -> bool:
    """Compares the driver's own result file, read back separately from
    stdout/stderr by the Spawner, so a submission's own print() calls can
    never be mistaken for the answer, and expected_output never has to enter
    the sandbox at all: this comparison runs here, outside it, after the
    sandbox has already finished and returned."""
    raw = exec_result.get("result")
    if not raw:
        return False
    try:
        actual = json.loads(raw)
        expected = json.loads(expected_output)
    except ValueError:
        return False
    return _values_equal(actual, expected, return_type)


def _values_equal(actual, expected, type_: str) -> bool:
    if type_.endswith("[]"):
        element_type = type_[:-2]
        if not isinstance(actual, list) or not isinstance(expected, list) or len(actual) != len(expected):
            return False
        return all(_values_equal(a, e, element_type) for a, e in zip(actual, expected))
    if type_ == "float":
        if not isinstance(actual, (int, float)) or not isinstance(expected, (int, float)):
            return False
        # Relative tolerance, with a floor so comparisons near zero aren't impossibly strict.
        return abs(actual - expected) <= 1e-6 * max(1, abs(expected))
    return actual == expected
