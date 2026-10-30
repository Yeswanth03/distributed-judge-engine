from pydantic import BaseModel


class ExecuteRequest(BaseModel):
    submission_id: str
    language: str
    source_code: str
    stdin: str = ""
    time_limit_ms: int
    memory_limit_mb: int


class ExecuteResult(BaseModel):
    stdout: str
    stderr: str
    exit_code: int | None
    wall_time_ms: int
    peak_memory_kb: int | None
    timed_out: bool
    oom_killed: bool
