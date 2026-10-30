from datetime import datetime, timezone

from .catalog_client import CatalogClient
from .models import Job, TestResult, Verdict
from .spawner_client import SpawnerClient


def judge_submission(job: Job, catalog: CatalogClient, spawner: SpawnerClient, worker_id: str) -> Verdict:
    problem_id = catalog.get_problem_id(job.problem_id)
    testset = catalog.get_testset(problem_id)

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
            stdin=case["input"],
            time_limit_ms=testset["time_limit_ms"],
            memory_limit_mb=testset["memory_limit_mb"],
        )

        case_verdict = _classify(exec_result, case["expected_output"])
        passed = case_verdict == "ACCEPTED"

        results.append(
            TestResult(
                test_case_ordinal=case["ordinal"],
                passed=passed,
                time_ms=exec_result["wall_time_ms"],
                memory_kb=exec_result.get("peak_memory_kb"),
            )
        )
        total_time_ms += exec_result["wall_time_ms"]
        max_memory_kb = max(max_memory_kb, exec_result.get("peak_memory_kb") or 0)

        if not passed:
            verdict = case_verdict
            if verdict == "RUNTIME_ERROR":
                # Not stderr: a submission could print the hidden test input there.
                error_message = f"Program exited with code {exec_result['exit_code']}"
            break  # stop at the first failing case, like most judges do

    return Verdict(
        submission_id=job.submission_id,
        user_id=job.user_id,
        problem_id=problem_id,
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


def _classify(exec_result: dict, expected_output: str) -> str:
    """The verdict values in docs/contracts.md. Catalog's VerdictMessageParser
    lowercases whatever it receives, so this suits both consumers."""
    if exec_result["timed_out"]:
        return "TIME_LIMIT_EXCEEDED"
    if exec_result["oom_killed"]:
        return "MEMORY_LIMIT_EXCEEDED"
    if exec_result["exit_code"] != 0:
        return "RUNTIME_ERROR"
    if exec_result["stdout"].strip() != expected_output.strip():
        return "WRONG_ANSWER"
    return "ACCEPTED"
