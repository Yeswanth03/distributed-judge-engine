"""Real-Docker integration tests, the exact scenarios manually verified during
development: a correct run, a genuine timeout, a submission that exceeds the
*problem's* memory limit without hitting the container's hard cap, and a
submission that genuinely gets OOM-killed by the kernel.

These need a real Docker daemon running and will pull python:3.11-slim on first
run. Skipped automatically (not failed) if Docker isn't reachable, so the fast
unit tests in test_pool.py still run fine without Docker.

Run just these:  pytest tests/test_docker_integration.py -v
"""

import docker
import pytest

from app.docker_runner import create_container, run_submission
from app.models import ExecuteRequest


def _docker_available() -> bool:
    try:
        docker.from_env().ping()
        return True
    except Exception:
        return False


pytestmark = pytest.mark.skipif(not _docker_available(), reason="Docker daemon not reachable")


@pytest.fixture
def container():
    client = docker.from_env()
    c = create_container(client, "python:3.11-slim", memory_mb=512, cpus=1.0)
    yield c
    c.remove(force=True)


def test_correct_submission_is_accepted(container):
    req = ExecuteRequest(
        submission_id="s1",
        language="python3.11",
        source_code="a, b = map(int, input().split())\nprint(a + b)",
        stdin="2 3\n",
        time_limit_ms=2000,
        memory_limit_mb=128,
    )
    result = run_submission(container, req)
    assert result.stdout.strip() == "5"
    assert result.exit_code == 0
    assert not result.timed_out
    assert not result.oom_killed


def test_infinite_loop_times_out(container):
    req = ExecuteRequest(
        submission_id="s2",
        language="python3.11",
        source_code="while True: pass",
        stdin="",
        time_limit_ms=1000,
        memory_limit_mb=128,
    )
    result = run_submission(container, req)
    assert result.timed_out is True
    assert result.oom_killed is False
    # the timeout wrapper should fire close to the limit, not way over it
    assert result.wall_time_ms < 3000


def test_exceeds_problem_memory_limit_without_hitting_container_cap(container):
    # 200MB allocation, declared limit 64MB, but the container's own hard cap is
    # 512MB, so the kernel never kills it. This is exactly the bug the smoke test
    # found: memory must be checked independently of whether anything got killed.
    req = ExecuteRequest(
        submission_id="s3",
        language="python3.11",
        source_code="x = bytearray(200 * 1024 * 1024)\nprint(len(x))",
        stdin="",
        time_limit_ms=5000,
        memory_limit_mb=64,
    )
    result = run_submission(container, req)
    assert result.exit_code == 0  # the process itself ran to completion
    assert result.oom_killed is True  # but it's still over the declared limit


def test_genuine_oom_kill_by_kernel(container):
    # 700MB allocation against the container's 512MB hard cap: this one should
    # actually be killed by the kernel's OOM killer.
    req = ExecuteRequest(
        submission_id="s4",
        language="python3.11",
        source_code="x = bytearray(700 * 1024 * 1024)\nprint('done')",
        stdin="",
        time_limit_ms=5000,
        memory_limit_mb=256,
    )
    result = run_submission(container, req)
    assert result.exit_code == 137
    assert result.oom_killed is True
    assert result.timed_out is False


def test_driver_runs_in_the_real_sandbox_and_result_is_separate_from_stdout(container):
    # A function-style problem (docs/contracts.md section 5): the submission
    # alone (solution.py) never runs; the driver does, imports it, and writes
    # its own result file inside the real read-only-rootfs, no-network
    # container, same as everything else does.
    req = ExecuteRequest(
        submission_id="s5",
        language="python3.11",
        source_code="class Solution:\n    def twoSum(self, nums, target):\n        print('debug noise')\n        return [0, 1]\n",
        driver_code=(
            "import json\nimport sys\nfrom solution import Solution\n\n"
            "args = json.loads(sys.stdin.readline())\n"
            "result = Solution().twoSum(**args)\n"
            "with open('result.json', 'w') as f:\n    json.dump(result, f)\n"
        ),
        stdin='{"nums": [2, 7, 11, 15], "target": 9}\n',
        time_limit_ms=2000,
        memory_limit_mb=128,
    )
    result = run_submission(container, req)
    assert result.exit_code == 0
    assert result.result == "[0, 1]"
    # The debug print landed on stdout, nowhere near the result file.
    assert "debug noise" in result.stdout
    assert "debug" not in result.result
