from datetime import datetime, timezone

from .catalog_client import CatalogClient
from .models import Job, TestResult, Verdict
from .spawner_client import SpawnerClient


def judge_submission(job: Job, catalog: CatalogClient, spawner: SpawnerClient, worker_id: str) -> Verdict:
    testset = catalog.get_testset(job.problem_id)

    results: list[TestResult] = []
    verdict = "accepted"
    total_time_ms = 0
    max_memory_kb = 0

    for case in testset["cases"]:
        exec_result = spawner.execute(
            submission_id=job.submission_id,
            language=job.language,
            source_code=job.source_code,
            stdin=case["input"],
            time_limit_ms=testset["time_limit_ms"],
            memory_limit_mb=testset["memory_limit_mb"],
        )

        case_verdict = _classify(exec_result, case["expected_output"])
        passed = case_verdict == "accepted"

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
            break  # stop at the first failing case, like most judges do

    return Verdict(
        submission_id=job.submission_id,
        user_id=job.user_id,
        problem_id=job.problem_id,
        testset_version=testset["version"],
        verdict=verdict,
        rejudge_seq=0,  # always 0 until re-judging a submission is built
        judged_at=datetime.now(timezone.utc).isoformat(),
        test_results=results,
        total_time_ms=total_time_ms,
        max_memory_kb=max_memory_kb or None,
        content_hash=testset["content_hash"],
        worker_id=worker_id,
    )


def _classify(exec_result: dict, expected_output: str) -> str:
    """Lowercase snake_case values, matching Catalog's VerdictMessageParser and
    its 'verdict.<status>' routing key convention exactly."""
    if exec_result["timed_out"]:
        return "time_limit_exceeded"
    if exec_result["oom_killed"]:
        return "memory_limit_exceeded"
    if exec_result["exit_code"] != 0:
        return "runtime_error"
    if exec_result["stdout"].strip() != expected_output.strip():
        return "wrong_answer"
    return "accepted"
