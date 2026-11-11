"""Builds the driver for a function-style submission (docs/contracts.md
section 5): problems are functions to implement, not stdin/stdout programs.

The driver is a separate script from the submission, never appended to it:
it imports the submitted `Solution` class, calls the declared function with
the case's JSON arguments, and writes the result to its own file
(result.json), never to stdout. A submission's own print() calls (debugging,
usually) land on stdout and can never be mistaken for the answer, because
the answer was never there to begin with. See Spawner's docker_runner.py for
how the driver and the submission run together and the result file is read
back separately from stdout/stderr.
"""

DRIVER_TEMPLATE = """import json
import sys
from solution import Solution


def _run_driver():
    args = json.loads(sys.stdin.readline())
    result = Solution().{function_name}(**args)
    with open("result.json", "w") as f:
        json.dump(result, f)


_run_driver()
"""


def build_driver(function_name: str) -> str:
    """function_name was already validated as a safe Python identifier by
    Catalog's FunctionRules before the problem was published, so no escaping
    is needed here."""
    return DRIVER_TEMPLATE.format(function_name=function_name)
