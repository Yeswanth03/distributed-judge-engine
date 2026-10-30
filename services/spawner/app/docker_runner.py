"""Everything that touches the Docker API directly. Nothing outside this module
should ever call the docker SDK, keeping the privileged surface in one place.
"""

import socket
import time
import uuid

from docker.errors import APIError, NotFound

from .models import ExecuteRequest, ExecuteResult

WORKDIR = "/workspace/run"

# Language -> command used to run a submission inside the sandbox, relative to WORKDIR.
# Only Python is wired up for this project; add an entry here before accepting another language.
# "python" is the name jobs use (docs/contracts.md).
RUN_COMMANDS = {
    "python": "python3 solution.py",
    "python3.11": "python3 solution.py",
}
SOURCE_FILENAMES = {
    "python": "solution.py",
    "python3.11": "solution.py",
}


def create_container(client, image: str, memory_mb: int, cpus: float):
    """Starts one warm, locked-down sandbox container that sits idle until claimed.

    Security posture (full checklist lives in the design doc):
      - no network at all (network_disabled)
      - read-only root filesystem; only /workspace is writable, and it's tmpfs
        (never touches host disk, wiped on container removal for free)
      - every Linux capability dropped, no new privileges obtainable
      - non-root user
      - pids/memory/cpu capped at the container level as a hard safety ceiling,
        independent of whatever time/memory limit an individual problem asks for
    """
    container = client.containers.run(
        image,
        command=["sleep", "infinity"],
        detach=True,
        # Otherwise Docker assigns a random name (quizzical_borg, etc.), making
        # it impossible to tell our sandboxes apart from anything else running
        # on the host at a glance (e.g. in Docker Desktop's container list).
        name=f"spawner-sandbox-{uuid.uuid4().hex[:8]}",
        network_disabled=True,
        read_only=True,
        tmpfs={"/workspace": "rw,size=64m,exec"},
        mem_limit=f"{memory_mb}m",
        memswap_limit=f"{memory_mb}m",  # no swap beyond the memory limit
        nano_cpus=int(cpus * 1_000_000_000),
        pids_limit=64,
        cap_drop=["ALL"],
        security_opt=["no-new-privileges"],
        user="nobody",
    )
    container.exec_run(["mkdir", "-p", WORKDIR])
    return container


def is_alive(container) -> bool:
    try:
        container.reload()
        return container.status == "running"
    except (NotFound, APIError):
        return False


def wipe_and_health_check(container) -> bool:
    """Clears the workspace before a container is marked reusable. False means the
    container isn't trustworthy to reuse; the caller should destroy it instead."""
    if not is_alive(container):
        return False
    try:
        exit_code, _ = container.exec_run(["sh", "-c", f"rm -rf {WORKDIR} && mkdir -p {WORKDIR}"])
        return exit_code == 0
    except APIError:
        return False


def run_submission(container, req: ExecuteRequest) -> ExecuteResult:
    if req.language not in RUN_COMMANDS:
        raise ValueError(f"unsupported language: {req.language}")

    source_name = SOURCE_FILENAMES[req.language]
    run_cmd = RUN_COMMANDS[req.language]

    # Files go in via live exec + stdin, never a shell heredoc (a submission's own
    # source could break out of a naively-quoted one, e.g. a line that's just "EOF")
    # and never Docker's archive API: put_archive/get_archive both outright refuse
    # to touch a read-only-rootfs container, even for a path that's a genuinely
    # writable tmpfs mount, confirmed directly against the Docker daemon. Piping
    # through a live process's stdin/stdout works fine since it goes through the
    # container's actual running mount namespace instead.
    _put_file(container, WORKDIR, source_name, req.source_code)
    _put_file(container, WORKDIR, "stdin.txt", req.stdin)

    time_limit_s = max(1, round(req.time_limit_ms / 1000))
    # `timeout -s KILL` kills the whole process tree on expiry. The memory.peak read
    # assumes cgroup v2 (default on current Docker Desktop/Linux); if it's missing we
    # just skip memory enforcement rather than fail the run.
    shell_cmd = (
        f"cd {WORKDIR} && "
        f"timeout -s KILL {time_limit_s}s {run_cmd} < stdin.txt > stdout.txt 2> stderr.txt; "
        f"echo $? > exit_code.txt; "
        f"cat /sys/fs/cgroup/memory.peak 2>/dev/null > mem_peak.txt || echo -1 > mem_peak.txt"
    )

    start = time.monotonic()
    container.exec_run(["sh", "-c", shell_cmd])
    wall_time_ms = int((time.monotonic() - start) * 1000)

    stdout = _read_file(container, f"{WORKDIR}/stdout.txt")
    stderr = _read_file(container, f"{WORKDIR}/stderr.txt")
    exit_code_raw = _read_file(container, f"{WORKDIR}/exit_code.txt").strip()
    mem_peak_raw = _read_file(container, f"{WORKDIR}/mem_peak.txt").strip()

    exit_code = int(exit_code_raw) if exit_code_raw.lstrip("-").isdigit() else None
    try:
        peak_memory_kb = int(mem_peak_raw) // 1024 if mem_peak_raw not in ("", "-1") else None
    except ValueError:
        peak_memory_kb = None

    # Memory is checked independently of exit code: the container's own cgroup cap
    # is a generous safety ceiling (set per settings.container_memory_mb), well
    # above any single problem's declared memory_limit_mb, so a submission can
    # exceed the problem's limit without ever being killed by the kernel. If we
    # only checked "was it killed", that submission would wrongly pass. So peak
    # memory is always compared against the declared limit, and the kill-based
    # check (`timeout`'s SIGKILL and the kernel's OOM-killer share the same exit
    # code, 137) only disambiguates what caused a kill, not whether the limit was
    # exceeded.
    was_killed = exit_code == 137
    memory_exceeded = peak_memory_kb is not None and peak_memory_kb >= req.memory_limit_mb * 1024
    oom_killed = memory_exceeded
    timed_out = was_killed and not memory_exceeded

    return ExecuteResult(
        stdout=stdout,
        stderr=stderr,
        exit_code=exit_code,
        wall_time_ms=wall_time_ms,
        peak_memory_kb=peak_memory_kb,
        timed_out=timed_out,
        oom_killed=oom_killed,
    )


def _put_file(container, path: str, filename: str, content: str, timeout_s: float = 10.0):
    data = content.encode("utf-8")
    full_path = f"{path}/{filename}"
    api = container.client.api
    exec_id = api.exec_create(container.id, ["sh", "-c", f"cat > {full_path}"], stdin=True)["Id"]
    sock = api.exec_start(exec_id, socket=True)
    try:
        raw = sock._sock  # the SocketIO wrapper docker-py returns is read-only; write the raw socket
        raw.sendall(data)
        raw.shutdown(socket.SHUT_WR)
    finally:
        sock.close()
    _wait_for_exec(api, exec_id, timeout_s)


def _wait_for_exec(api, exec_id: str, timeout_s: float) -> int:
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        info = api.exec_inspect(exec_id)
        if not info["Running"]:
            return info["ExitCode"]
        time.sleep(0.01)
    raise TimeoutError(f"exec {exec_id} did not finish in time")


def _read_file(container, path: str) -> str:
    try:
        exit_code, output = container.exec_run(["cat", path])
        if exit_code != 0:
            return ""
        return output.decode("utf-8", errors="replace")
    except (NotFound, APIError):
        return ""
