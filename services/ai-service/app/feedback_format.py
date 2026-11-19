"""The structured feedback format: the model fills fixed fields, and this module turns
them into the text the learner sees.

Asking a small model to *write* a multi-section template is unreliable: it drops the
checklist line, merges sections or runs long. Instead, Ollama is given FeedbackReport's
JSON schema as `format`, which constrains generation so the reply is always valid JSON
with exactly these fields. The layout, the ✓/✗ marks and the summary rules are then
applied here, in code, so they hold whatever the model writes.

The fields are flat (no nesting) and in the order the text shows them, which keeps the
model's job simple.
"""

import json
import re

from pydantic import BaseModel, Field, ValidationError

CONGRATULATIONS = "Congratulations! You passed on this attempt. Your solution is efficient and correct."
UNCLEAR = "Unclear"

# Which checklist item a failed verdict must mark ✗ (the prompt's checklist rules).
_FAILED_ITEM = {
    "wrong_answer": "approach_ok",
    "runtime_error": "approach_ok",
    "compilation_error": "approach_ok",
    "time_limit_exceeded": "efficiency_ok",
    "memory_limit_exceeded": "efficiency_ok",
}

_FIELD_WORDS = 30   # every field is "one sentence or less"
_CODE_BLOCK = re.compile(r"```.*?(?:```|\Z)", re.DOTALL)


class FeedbackReport(BaseModel):
    approach_ok: bool
    efficiency_ok: bool
    summary: str = Field(description="One sentence on the verdict and overall quality")
    approach_current: str
    approach_suggested: str
    approach_key_idea: str
    approach_consider: str
    efficiency_current_time: str
    efficiency_current_space: str
    efficiency_optimal_time: str
    efficiency_optimal_space: str
    efficiency_key_idea: str
    efficiency_consider: str


def json_schema() -> dict:
    """What Ollama's `format` option receives. Every field is required."""
    return FeedbackReport.model_json_schema()


# What each field means, for the prompt (the schema itself only fixes the shape).
FIELD_GUIDE = """Fill these JSON fields; each is one line of the format above:
- approach_ok, efficiency_ok: true for ✓, false for ✗, following the checklist rules
- summary: the one-sentence summary line
- approach_current, approach_suggested, approach_key_idea, approach_consider: the Approach section
- efficiency_current_time, efficiency_current_space, efficiency_optimal_time, efficiency_optimal_space: complexities such as "O(n)"
- efficiency_key_idea, efficiency_consider: the rest of the Efficiency section
Every *_consider field is a question. Use "Unclear" for a field you cannot judge."""


def parse(raw: str) -> FeedbackReport:
    """Raises ValueError if the reply isn't the expected JSON (treated like any bad response)."""
    try:
        return FeedbackReport.model_validate_json(raw)
    except ValidationError as exc:
        raise ValueError(f"Ollama returned feedback that doesn't match the format: {exc.error_count()} error(s)") from exc


def _clean(value: str, max_words: int = _FIELD_WORDS) -> str:
    """One tidy line: code blocks removed, whitespace collapsed, at most max_words words."""
    value = _CODE_BLOCK.sub("", value)
    value = " ".join(value.split())
    words = value.split(" ")
    if len(words) > max_words:
        value = " ".join(words[:max_words]).rstrip(",;:") + "…"
    return value or UNCLEAR


def _first_sentence(value: str) -> str:
    value = _clean(value)
    match = re.search(r"[.!?](?=\s|$)", value)
    return value[: match.end()] if match else value


def _question(value: str) -> str:
    """'Consider' lines are questions: keep up to the first question mark."""
    value = _clean(value)
    end = value.find("?")
    return value[: end + 1] if end != -1 else value


def _complexity(value: str) -> str:
    return _clean(value, max_words=8)


def apply_rules(report: FeedbackReport, verdict: str) -> FeedbackReport:
    """The prompt's checklist and summary rules, enforced whatever the model wrote."""
    data = report.model_dump()
    failed_item = _FAILED_ITEM.get(verdict)
    if failed_item and data["approach_ok"] and data["efficiency_ok"]:
        # Not accepted, yet both marked ✓: mark the item this verdict points to.
        data[failed_item] = False
    if verdict == "accepted" and data["approach_ok"] and data["efficiency_ok"]:
        data["summary"] = CONGRATULATIONS
    return FeedbackReport(**data)


# ---- output guardrails: checked before a hint is saved or shown ----

class HintBlocked(ValueError):
    """The hint failed a guardrail. A ValueError, so it is handled like any bad model
    reply: generation is retried, and after the last attempt the request is unavailable.
    It is not an outage, so the Ollama circuit breaker ignores it."""

    counts_as_outage = False


# Code the model must never hand over. Each pattern needs real syntax (brackets, a colon,
# an assignment), so prose such as "check the return statement" or "the for loop" passes.
_CODE_PATTERNS = [
    re.compile(r"\bdef\s+\w+\s*\("),                                    # def twoSum(
    re.compile(r"\bclass\s+\w+\s*[:(]"),                                 # class Solution:
    re.compile(r"\breturn\s+[\[(\w][^.?!]*?[\])]"),                        # return [i, j]
    re.compile(r"\b(?:for|while)\b[^.?!]*:\s*(?:$|\w)"),                    # for i in range(n):
    re.compile(r"\w+\s*=\s*(?:\{\}|\[\]|dict\(\)|set\(\)|\w+\[)"),          # seen = {}  /  x = nums[
    re.compile(r"\w+\[[^\]]+\]\s*=\s*\w"),                                 # seen[x] = i
]
# Hidden test data shorter than this is too common to check reliably ("0 1", "[2,3]", "17"):
# it would also appear in ordinary prose. Longer values are distinctive.
_MIN_HIDDEN_CHARS = 8


def looks_like_code(text: str) -> bool:
    return any(pattern.search(text) for pattern in _CODE_PATTERNS)


def redact_code(report: FeedbackReport) -> tuple[FeedbackReport, int]:
    """Removes code from every text field: fenced code blocks are cut out, and a field
    that still contains code in its prose is replaced with "Unclear". Returns the cleaned
    report and how many fields were changed (for the guardrail metric)."""
    data, redacted = report.model_dump(), 0
    for name, value in data.items():
        if not isinstance(value, str):
            continue
        cleaned = _CODE_BLOCK.sub("", value)
        if looks_like_code(cleaned):
            cleaned = UNCLEAR
        if cleaned != value:
            data[name] = cleaned.strip() or UNCLEAR
            redacted += 1
    return FeedbackReport(**data), redacted


def _squash(text: str) -> str:
    return re.sub(r"\s+", "", text).lower()


def _hidden_values(case) -> list[str]:
    """A hidden case's input and expected output, plus each argument of a JSON input
    ({"nums": [...], "target": 17}), since a leak could quote just one of them."""
    values = [case.input, case.expected_output]
    try:
        parsed = json.loads(case.input)
    except (ValueError, TypeError):
        parsed = None
    if isinstance(parsed, dict):
        values += [json.dumps(v, separators=(",", ":")) for v in parsed.values()]
    return [v for v in (_squash(str(v)) for v in values) if len(v) >= _MIN_HIDDEN_CHARS]


def leaks_hidden_tests(text: str, cases) -> bool:
    """True if the text contains any hidden test case's data. Hidden cases are never put
    in the prompt, so this is a second line of defence, not the first."""
    squashed = _squash(text)
    return any(value in squashed for case in cases if not case.is_sample for value in _hidden_values(case))


def render(report: FeedbackReport) -> str:
    """The learner-facing text, in exactly the agreed layout."""
    mark = lambda ok: "✓" if ok else "✗"  # noqa: E731
    r = report
    return "\n\n".join([
        f"{mark(r.approach_ok)} Approach   {mark(r.efficiency_ok)} Efficiency\n"
        f"{r.summary if r.summary == CONGRATULATIONS else _first_sentence(r.summary)}",
        "Approach\n"
        f"Current: {_clean(r.approach_current, 8)}\n"
        f"Suggested: {_clean(r.approach_suggested, 8)}\n"
        f"Key Idea: {_first_sentence(r.approach_key_idea)}\n"
        f"Consider: {_question(r.approach_consider)}",
        "Efficiency\n"
        f"Current: Time {_complexity(r.efficiency_current_time)}, Space {_complexity(r.efficiency_current_space)}\n"
        f"Optimal: Time {_complexity(r.efficiency_optimal_time)}, Space {_complexity(r.efficiency_optimal_space)}\n"
        f"Key Idea: {_first_sentence(r.efficiency_key_idea)}\n"
        f"Consider: {_question(r.efficiency_consider)}",
    ])
