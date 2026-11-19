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
