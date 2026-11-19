#!/usr/bin/env python3
"""Chaos tests for the AI service: break its dependencies on purpose and check that
it degrades and recovers the way it was designed to.

  1. ollama      Stop the LLM. Failed verdicts keep arriving: the Ollama circuit
                 opens, requests are deferred (not failed), and once Ollama is back
                 the circuit closes and the deferred feedback is generated.
  2. submission  Stop the Submission Service. Feedback requests get 502 at first,
                 then the circuit opens and they fail fast with 503 circuit_open and
                 Retry-After. After a restart, requests work again.
  3. crash       Kill the AI service in the middle of a generation. After a restart
                 the orphaned request is reclaimed by the sweep and completed.

Needs the whole judging path running, because the tests use real wrong-answer
submissions: from the repository root,

    docker compose up -d --wait ai-service

plus Catalog, the Spawner and the Judge Worker. Then run:

    python3 services/ai-service/scripts/chaos.py                # all three tests
    python3 services/ai-service/scripts/chaos.py --tests crash  # just one

Watch the Grafana dashboard (http://localhost:3000) while it runs. Only the
standard library is used, so any Python 3.9+ works.
"""

from __future__ import annotations

import argparse
import json
import shlex
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

TWO_SUM = "a68b526a-ed8b-55f7-8113-80baab531c65"  # sample problem shipped with Catalog
WRONG_ANSWER = "class Solution:\n    def twoSum(self, nums, target):\n        return [0, 0]\n"


class Chaos:
    def __init__(self, args):
        self.ai = args.ai_url.rstrip("/")
        self.submissions = args.submission_url.rstrip("/")
        self.user = args.user_id
        self.problem = args.problem_id
        self.compose_cmd = shlex.split(args.compose)
        self.timeout = args.timeout
        self.results: list[tuple[str, bool, str]] = []

    # ---- plumbing ----

    def log(self, message: str) -> None:
        print(f"[{time.strftime('%H:%M:%S')}] {message}", flush=True)

    def http(self, method: str, url: str, body: dict | None = None) -> tuple[int, dict | None, dict]:
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(url, data=data, method=method, headers={
            "X-User-Id": self.user, "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=10) as resp:
                raw = resp.read()
                return resp.status, json.loads(raw) if raw else None, dict(resp.headers)
        except urllib.error.HTTPError as err:
            raw = err.read()
            try:
                payload = json.loads(raw) if raw else None
            except ValueError:
                payload = None
            return err.code, payload, dict(err.headers)
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            return 0, None, {}

    def compose(self, *args: str) -> None:
        self.log("$ " + " ".join(self.compose_cmd + list(args)))
        subprocess.run(self.compose_cmd + list(args), check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def wait_until(self, check, timeout: float, what: str, interval: float = 2.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            result = check()
            if result:
                return result
            time.sleep(interval)
        raise TimeoutError(f"timed out after {timeout:.0f}s waiting for {what}")

    def circuit(self, dependency: str) -> str | None:
        status, body, _ = self.http("GET", f"{self.ai}/health")
        if status != 200 or not body:
            return None
        return (body.get("circuits") or {}).get(dependency, {}).get("state")

    def feedback(self, submission_id: str) -> dict | None:
        status, body, _ = self.http("GET", f"{self.ai}/submissions/{submission_id}/feedback")
        return body if status == 200 else None

    def submit_wrong_answer(self) -> str:
        status, body, _ = self.http("POST", f"{self.submissions}/api/v1/submissions",
                                    {"problem_id": self.problem, "language": "python", "code": WRONG_ANSWER})
        if status != 202:
            raise RuntimeError(f"Submission Service answered {status}: {body}")
        sid = body["submission_id"]

        def judged():
            _, sub, _ = self.http("GET", f"{self.submissions}/api/v1/submissions/{sid}")
            return sub if sub and sub.get("status") == "COMPLETED" else None

        sub = self.wait_until(judged, 90, f"submission {sid[:8]} to be judged (is the Judge Worker running?)", 1)
        self.log(f"submission {sid[:8]} judged {sub['verdict']} ({sub['test_cases_passed']}/{sub['total_test_cases']})")
        return sid

    def record(self, name: str, ok: bool, detail: str) -> None:
        self.results.append((name, ok, detail))
        self.log(("PASS " if ok else "FAIL ") + f"{name}: {detail}")

    # ---- the tests ----

    def test_ollama(self) -> None:
        self.log("=== 1. Ollama outage: circuit opens, requests are deferred, then recover")
        self.compose("stop", "ollama")
        started = time.monotonic()
        try:
            sids = [self.submit_wrong_answer() for _ in range(3)]
            self.wait_until(lambda: self.circuit("ollama") == "open", 180, "the Ollama circuit to open")
            self.log(f"Ollama circuit OPEN after {time.monotonic() - started:.0f}s")
            waiting = [self.feedback(s) for s in sids]
            for sid, fb in zip(sids, waiting):
                self.log(f"  feedback {sid[:8]}: {fb and fb['status']} - {fb and fb.get('message')}")
        finally:
            self.compose("up", "-d", "--wait", "ollama")
        deferred = sum(1 for fb in waiting if fb and fb["status"] == "queued")
        recovered_at = time.monotonic()
        self.wait_until(lambda: self.circuit("ollama") == "closed", self.timeout, "the Ollama circuit to close")
        self.log(f"Ollama circuit CLOSED {time.monotonic() - recovered_at:.0f}s after Ollama came back")
        # Requests that used up their retries before the circuit opened ended "unavailable";
        # asking again (what the user's "get feedback" button does) starts a new attempt.
        # Deferred and ready requests are simply returned as they are.
        for sid in sids:
            self.http("POST", f"{self.ai}/submissions/{sid}/feedback")
        ready = self.wait_until(
            lambda: (all((fb := self.feedback(s)) and fb["status"] == "ready" for s in sids) or None),
            self.timeout, "all feedback to be generated", 5)
        self.record("ollama outage", bool(ready) and deferred > 0,
                    f"circuit opened, {deferred} of {len(sids)} requests deferred instead of failed, "
                    f"all ready {time.monotonic() - recovered_at:.0f}s after recovery")

    def test_submission(self) -> None:
        self.log("=== 2. Submission Service outage: 502, then fail fast with 503 circuit_open")
        sid = self.submit_wrong_answer()
        self.compose("stop", "submission-service")
        codes, retry_after = [], None
        try:
            for _ in range(10):
                status, body, headers = self.http("POST", f"{self.ai}/submissions/{sid}/feedback")
                codes.append(status)
                if status == 503 and body and body.get("status") == "circuit_open":
                    retry_after = int(headers.get("Retry-After", body.get("retry_after", 10)))
                    break
                time.sleep(0.5)
            self.log(f"responses while down: {codes}; Retry-After: {retry_after}")
        finally:
            self.compose("up", "-d", "--wait", "submission-service")
        time.sleep((retry_after or 10) + 1)
        status, body, _ = self.http("POST", f"{self.ai}/submissions/{sid}/feedback")
        recovered = status in (200, 202)
        self.log(f"after restart and Retry-After: HTTP {status} ({body and body.get('status')})")
        self.record("submission outage", retry_after is not None and recovered,
                    f"502 x{codes.count(502)}, then 503 circuit_open; after recovery HTTP {status}")

    def test_crash(self) -> None:
        self.log("=== 3. AI service killed mid-generation: the sweep reclaims the request")
        sid = self.submit_wrong_answer()
        self.wait_until(lambda: (fb := self.feedback(sid)) and fb["status"] == "processing",
                        60, "generation to start", 0.3)
        self.log(f"feedback {sid[:8]} is processing; killing the AI service")
        self.compose("kill", "ai-service")
        self.compose("up", "-d", "--wait", "ai-service")
        orphan = self.feedback(sid)
        self.log(f"after restart: {orphan and orphan['status']} (no worker owns it any more); "
                 "waiting for the sweep to reclaim it after AI_PROCESSING_STALE_SECONDS")
        restarted = time.monotonic()
        fb = self.wait_until(lambda: (f := self.feedback(sid)) and f["status"] == "ready" and f,
                             self.timeout, "the orphaned request to complete", 5)
        self.record("crash mid-generation", bool(fb),
                    f"left in processing by the crash, ready {time.monotonic() - restarted:.0f}s after restart")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tests", default="ollama,submission,crash", help="comma-separated: ollama, submission, crash")
    parser.add_argument("--ai-url", default="http://localhost:8084")
    parser.add_argument("--submission-url", default="http://localhost:8082")
    parser.add_argument("--user-id", default=str(uuid.uuid4()), help="a fresh test user each run by default")
    parser.add_argument("--problem-id", default=TWO_SUM)
    parser.add_argument("--compose", default="docker compose", help='e.g. "docker compose -f docker-compose.yml -f my.yml"')
    parser.add_argument("--timeout", type=float, default=420, help="seconds to wait for each recovery")
    args = parser.parse_args()

    chaos = Chaos(args)
    status, body, _ = chaos.http("GET", f"{chaos.ai}/health")
    if status != 200:
        print(f"The AI service isn't answering at {chaos.ai}/health. Start it first (see the docstring).")
        return 2
    if "circuits" not in (body or {}):
        print("Note: this AI service has no circuit breakers; tests 1 and 2 need them.")

    tests = {"ollama": chaos.test_ollama, "submission": chaos.test_submission, "crash": chaos.test_crash}
    for name in [t.strip() for t in args.tests.split(",") if t.strip()]:
        try:
            tests[name]()
        except Exception as exc:  # report and carry on with the next test
            chaos.record(name, False, f"{type(exc).__name__}: {exc}")

    print("\n" + "=" * 70)
    for name, ok, detail in chaos.results:
        print(f"{'PASS' if ok else 'FAIL'}  {name:22} {detail}")
    return 0 if chaos.results and all(ok for _, ok, _ in chaos.results) else 1


if __name__ == "__main__":
    sys.exit(main())
