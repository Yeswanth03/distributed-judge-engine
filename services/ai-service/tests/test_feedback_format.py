import pytest

from app.feedback_format import CONGRATULATIONS, FeedbackReport, apply_rules, parse, render
from tests.test_ollama_client import report


def make(**overrides) -> FeedbackReport:
    return FeedbackReport(**report(**overrides))


# ---- the checklist and summary rules ----

@pytest.mark.parametrize("verdict, must_fail", [
    ("wrong_answer", "approach_ok"),
    ("runtime_error", "approach_ok"),
    ("compilation_error", "approach_ok"),
    ("time_limit_exceeded", "efficiency_ok"),
    ("memory_limit_exceeded", "efficiency_ok"),
])
def test_a_failed_verdict_never_shows_approach_and_efficiency_both_ticked(verdict, must_fail):
    fixed = apply_rules(make(approach_ok=True, efficiency_ok=True), verdict)
    assert getattr(fixed, must_fail) is False


def test_the_model_marking_the_other_item_is_respected():
    # Wrong answer, model already marked Efficiency ✗: that satisfies the rule, keep it.
    fixed = apply_rules(make(approach_ok=True, efficiency_ok=False), "wrong_answer")
    assert fixed.approach_ok is True and fixed.efficiency_ok is False


def test_there_is_no_code_style_section():
    from app.feedback_format import FeedbackReport
    assert not any("style" in name for name in FeedbackReport.model_fields)
    assert "Code Style" not in render(make())


def test_accepted_and_optimal_gets_the_standard_congratulations():
    fixed = apply_rules(make(approach_ok=True, efficiency_ok=True, summary="Great job, it works!"), "accepted")
    assert fixed.summary == CONGRATULATIONS


def test_accepted_but_not_optimal_keeps_the_models_summary():
    fixed = apply_rules(make(approach_ok=True, efficiency_ok=False,
                             summary="Accepted, but the nested loops make it O(n^2)."), "accepted")
    assert fixed.summary == "Accepted, but the nested loops make it O(n^2)."


# ---- the layout ----

def test_render_matches_the_agreed_format():
    assert render(make()) == (
        "✗ Approach   ✓ Efficiency\n"
        "The program prints 1-based positions, so the first sample fails.\n\n"
        "Approach\n"
        "Current: Hash map\n"
        "Suggested: Hash map\n"
        "Key Idea: Store each value's index and look up the complement.\n"
        "Consider: Which index does the problem count from?\n\n"
        "Efficiency\n"
        "Current: Time O(n), Space O(n)\n"
        "Optimal: Time O(n), Space O(n)\n"
        "Key Idea: Each value is visited once.\n"
        "Consider: Could any input make the lookup slower?"
    )


def test_fields_are_kept_to_one_sentence_and_one_question():
    text = render(make(
        summary="It fails. Also the style is fine. And more.",
        approach_consider="Which index is first? And why? Think about it.",
        efficiency_key_idea="Nested loops are quadratic. That is slow for 10^5 items.",
    ))
    assert "\nIt fails.\n" in text
    assert "Consider: Which index is first?\n" in text
    assert "Key Idea: Nested loops are quadratic.\n" in text


def test_long_or_empty_fields_are_capped_and_marked_unclear():
    text = render(make(approach_current="word " * 50, efficiency_key_idea="   "))
    current = next(line for line in text.splitlines() if line.startswith("Current: ") and "Time" not in line)
    assert len(current.split()) <= 10 and current.endswith("…")
    assert "Key Idea: Unclear" in text


def test_parse_rejects_missing_fields():
    with pytest.raises(ValueError):
        parse('{"approach_ok": true, "summary": "x"}')


def test_the_congratulations_summary_is_shown_in_full():
    # Its first sentence ends at "!", so one-sentence trimming must not apply to it.
    fixed = apply_rules(make(approach_ok=True, efficiency_ok=True), "accepted")
    assert "\n" + CONGRATULATIONS + "\n" in render(fixed)
