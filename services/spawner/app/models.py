from pydantic import BaseModel


class ExecuteRequest(BaseModel):
    submission_id: str
    language: str
    source_code: str
    # The judge's own driver for function-style problems (docs/contracts.md
    # section 5), run instead of source_code directly; None for a legacy
    # stdin/stdout problem, which runs source_code on its own.
    driver_code: str | None = None
    stdin: str = ""
    time_limit_ms: int
    memory_limit_mb: int


class ExecuteResult(BaseModel):
    stdout: str
    stderr: str
    # The driver's own result file, read back separately from stdout so a
    # submission's own print() calls can never corrupt the answer. None when
    # no driver ran, or when the driver crashed before writing it.
    result: str | None = None
    exit_code: int | None
    wall_time_ms: int
    peak_memory_kb: int | None
    timed_out: bool
    oom_killed: bool
