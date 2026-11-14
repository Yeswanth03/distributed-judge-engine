from pydantic import BaseModel


class Job(BaseModel):
    """What Submission Service puts on the queue. Note there's no time/memory limit
    here: those are fixed per-problem on Catalog and fetched from there, not carried
    on the job (see Catalog's /internal/problems/{id}/testset)."""

    submission_id: str
    problem_id: str
    user_id: str
    language: str
    code: str
    submitted_at: str


class RunningEvent(BaseModel):
    """Published when the worker starts a job, routing key `status.running`."""

    submission_id: str
    user_id: str
    problem_id: str
    status: str = "RUNNING"


class TestResult(BaseModel):
    test_case_ordinal: int
    passed: bool
    time_ms: int
    memory_kb: int | None = None

    # Only ever set for a FAILING sample case (judge.py checks case["is_sample"]
    # before populating any of these). Never set for a hidden case, passing or
    # not, so hidden test data can never leak through a verdict.
    input: str | None = None
    expected_output: str | None = None
    actual_output: str | None = None
    error: str | None = None


class Verdict(BaseModel):
    """Published to the 'verdicts' topic exchange, routing key `verdict.<verdict>`
    (lowercase). The status and result fields are what the Submission Service reads.
    Field names and the required subset (submission_id, user_id, problem_id,
    testset_version, verdict, rejudge_seq, judged_at) match Catalog's real
    VerdictMessageParser exactly, not a guess: extra fields below are fine,
    Catalog ignores anything it doesn't read, but the required ones and their
    exact names are a hard contract. rejudge_seq is 0 until re-judging
    submissions is built; Catalog uses it to ignore stale/duplicate verdicts."""

    submission_id: str
    user_id: str
    problem_id: str
    testset_version: int
    status: str = "COMPLETED"
    verdict: str  # e.g. "ACCEPTED", "WRONG_ANSWER" (see judge.py's _classify)
    rejudge_seq: int = 0
    judged_at: str
    execution_time_ms: int
    memory_kb: int | None
    test_cases_passed: int
    total_test_cases: int
    error_message: str | None = None

    # Not read by Catalog's listener, kept for our own observability/debugging.
    test_results: list[TestResult]
    content_hash: str
    worker_id: str


class RunJob(BaseModel):
    """What the Submission Service puts on runs.queue: a trial run, never
    scored. With no inputs the worker runs the problem's sample cases; with
    inputs it runs those instead (docs/contracts.md)."""

    run_id: str
    user_id: str
    problem_id: str
    language: str
    code: str
    inputs: list[str] | None = None
    requested_at: str


class RunCase(BaseModel):
    kind: str  # "sample" or "custom"
    input: str
    # Known for a sample, and for a custom input that matches a sample's.
    expected_output: str | None = None
    # The function's return value as JSON (stdout for a legacy stdin/stdout
    # problem); None when the code never got that far.
    output: str | None = None
    # The code's own print() output, kept apart from the answer.
    stdout: str = ""
    error: str | None = None
    # ACCEPTED or WRONG_ANSWER when the expected output is known, FINISHED when
    # it isn't, otherwise TIME_LIMIT_EXCEEDED, MEMORY_LIMIT_EXCEEDED,
    # RUNTIME_ERROR or INVALID_INPUT.
    verdict: str
    time_ms: int = 0
    memory_kb: int | None = None


class RunResult(BaseModel):
    """Sent straight back to the Submission Service on its run-results queue,
    never to the verdicts exchange: a run must never reach Catalog's leaderboard
    or the AI Service."""

    run_id: str
    status: str = "COMPLETED"
    # Over the cases with a known expected output; None when there were none.
    verdict: str | None
    cases: list[RunCase]
    finished_at: str
