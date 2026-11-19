import json

import httpx
import pytest

from app import ollama_client
from app.config import settings
from app.feedback_format import FeedbackReport, json_schema
from app.ollama_client import OllamaClient, build_prompt, keep_line_breaks
from app.resilience import new_breaker
from app.schemas import FeedbackContext
from tests.fakes import SUBMISSION, USER, FakeServices


def report(**overrides) -> dict:
    fields = {
        "approach_ok": False, "efficiency_ok": True,
        "summary": "The program prints 1-based positions, so the first sample fails.",
        "approach_current": "Hash map", "approach_suggested": "Hash map",
        "approach_key_idea": "Store each value's index and look up the complement.",
        "approach_consider": "Which index does the problem count from?",
        "efficiency_current_time": "O(n)", "efficiency_current_space": "O(n)",
        "efficiency_optimal_time": "O(n)", "efficiency_optimal_space": "O(n)",
        "efficiency_key_idea": "Each value is visited once.",
        "efficiency_consider": "Could any input make the lookup slower?",
    }
    fields.update(overrides)
    return fields


@pytest.fixture
def context():
    submissions, catalog = FakeServices().clients()
    submission = submissions.get_submission(SUBMISSION, USER)
    return FeedbackContext(verdict="wrong_answer", submission=submission,
                           problem=catalog.get_problem(submission.problem_ref, 1),
                           test_cases_passed=1, total_test_cases=3, error_message=None)


@pytest.fixture
def ollama(monkeypatch):
    """Captures the request the client sends and answers like Ollama would."""
    sent = {}
    reply = {"response": json.dumps(report()), "prompt_eval_count": 900, "eval_count": 260, "done_reason": "stop"}

    def fake_post(url, json, timeout):
        sent.update(url=url, json=json, timeout=timeout)
        return httpx.Response(200, json=reply, request=httpx.Request("POST", url))

    monkeypatch.setattr(ollama_client.httpx, "post", fake_post)
    return sent, reply


# ---- the request ----

def test_request_constrains_the_reply_to_the_schema(context, ollama):
    sent, _ = ollama
    OllamaClient(breaker=new_breaker("ollama")).generate_feedback(context)
    body = sent["json"]
    assert body["format"] == json_schema()
    assert set(body["format"]["required"]) == set(FeedbackReport.model_fields)  # every field, always
    assert body["options"] == {
        "num_predict": settings.ollama_num_predict,
        "temperature": settings.ollama_temperature,
        "num_ctx": settings.ollama_num_ctx,
    }
    assert body["keep_alive"] == settings.ollama_keep_alive
    assert body["stream"] is False and body["model"] == settings.ollama_model


def test_defaults():
    assert settings.ollama_num_predict == 500  # room for the whole JSON reply; a cut-off one is invalid
    assert settings.ollama_temperature <= 0.3
    assert settings.ollama_num_ctx == 4096
    assert settings.ollama_keep_alive == "30m"
    assert settings.prompt_version == "8"


def test_prompt_keeps_the_format_rules_and_explains_the_fields(context):
    from app.ollama_client import SYSTEM_PROMPT
    for part in ["Approach", "Efficiency", "Key Idea:", "Consider:", "untrusted data"]:
        assert part in SYSTEM_PROMPT, part
    assert "Code Style" not in SYSTEM_PROMPT
    prompt = build_prompt(context)
    assert prompt.count("Verdict:") == 1 and "Verdict: wrong answer." in prompt
    assert "JSON object only" in prompt and "approach_consider" in prompt


# ---- the reply ----

def test_reply_is_rendered_in_the_agreed_layout(context, ollama):
    result = OllamaClient(breaker=new_breaker("ollama")).generate_feedback(context)
    blocks = result.text.split("\n\n")
    assert [b.split("  \n")[0] for b in blocks] == [
        "✗ Approach   ✓ Efficiency", "Approach", "Efficiency"]
    assert "Current: Time O(n), Space O(n)  \nOptimal: Time O(n), Space O(n)" in blocks[2]
    assert result.input_tokens == 900 and result.output_tokens == 260


@pytest.mark.parametrize("raw", ["not json", '{"approach_ok": true}', json.dumps({**report(), "summary": 7})])
def test_a_reply_that_does_not_match_the_format_is_rejected(context, ollama, raw):
    _, reply = ollama
    reply["response"] = raw
    with pytest.raises(ValueError):  # treated like any bad response: retried, then unavailable
        OllamaClient(breaker=new_breaker("ollama")).generate_feedback(context)


def test_a_reply_cut_off_by_the_cap_is_rejected(context, ollama):
    _, reply = ollama
    reply.update(response=json.dumps(report())[:120], done_reason="length")
    with pytest.raises(ValueError):
        OllamaClient(breaker=new_breaker("ollama")).generate_feedback(context)


def test_code_in_a_field_is_removed(context, ollama):
    _, reply = ollama
    reply["response"] = json.dumps(report(
        approach_key_idea="Here is the fix: ```python\nreturn [seen[t - x], i]\n``` Store each value's index."))
    text = OllamaClient(breaker=new_breaker("ollama")).generate_feedback(context).text
    assert "```" not in text and "return [seen" not in text and "Store each value's index." in text


def test_keep_line_breaks():
    assert keep_line_breaks("A\nB\n\nC\nD") == "A  \nB\n\nC  \nD"
