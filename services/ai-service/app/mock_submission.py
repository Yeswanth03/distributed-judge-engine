from dataclasses import dataclass
from uuid import UUID

from .models import FailedVerdictEvent


@dataclass(frozen=True)
class SubmissionContext:
    submission_id: UUID
    user_id: str
    problem_id: UUID
    language: str
    source_code: str
    problem_title: str
    problem_statement: str
    constraints: str


class MockSubmissionProvider:
    """Temporary fixture provider; replace with the internal Submission/Catalog APIs."""

    def fetch(
        self,
        submission_id: UUID,
        user_id: str,
        problem_id: UUID | None = None,
    ) -> SubmissionContext:
        # This fixture is returned for any event ID during local development.
        # It deliberately contains no hidden tests or expected outputs.
        return SubmissionContext(
            submission_id=submission_id,
            user_id=user_id,
            problem_id=problem_id or UUID("db2353a5-0000-4000-8000-000000000001"),
            language="python3.11",
            problem_title="Two Sum",
            problem_statement=(
                "Given a list of integers and a target, return the indices of two "
                "different elements whose values add up to the target."
            ),
            constraints="The input list may contain duplicate values.",
            source_code=(
                "def two_sum(nums, target):\n"
                "    for i in range(len(nums)):\n"
                "        for j in range(i + 1, len(nums)):\n"
                "            if nums[i] + nums[j] == target:\n"
                "                return [i, j]\n"
                "    return []\n"
            ),
        )
