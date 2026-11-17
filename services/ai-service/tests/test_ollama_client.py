import httpx
import pytest

from app import ollama_client
from app.config import settings
from app.ollama_client import OllamaClient, trim_cut_off
from app.schemas import FeedbackContext
from tests.fakes import SUBMISSION, USER, FakeServices


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
    reply = {"response": "- Check the operator on the last line.", "prompt_eval_count": 700,
             "eval_count": 40, "done_reason": "stop"}

    def fake_post(url, json, timeout):
        sent.update(url=url, json=json, timeout=timeout)
        return httpx.Response(200, json=reply, request=httpx.Request("POST", url))

    monkeypatch.setattr(ollama_client.httpx, "post", fake_post)
    return sent, reply


def test_request_caps_output_and_keeps_the_model_loaded(context, ollama):
    sent, _ = ollama
    OllamaClient().generate_feedback(context)
    body = sent["json"]
    assert body["options"] == {
        "num_predict": settings.ollama_num_predict,
        "temperature": settings.ollama_temperature,
        "num_ctx": settings.ollama_num_ctx,
    }
    assert body["keep_alive"] == settings.ollama_keep_alive
    assert body["stream"] is False and body["model"] == settings.ollama_model


def test_defaults_are_tuned_for_short_focused_hints():
    assert settings.ollama_num_predict == 300  # ~150 words asked for in the prompt, plus margin
    assert settings.ollama_temperature <= 0.3
    assert settings.ollama_num_ctx == 4096
    assert settings.ollama_keep_alive == "30m"


def test_a_complete_answer_is_kept_as_it_is(context, ollama):
    _, reply = ollama
    reply["response"] = "- First point.\n- Second point"
    assert OllamaClient().generate_feedback(context).text == "- First point.\n- Second point"


def test_an_answer_cut_by_the_cap_ends_cleanly(context, ollama):
    _, reply = ollama
    reply.update(response="- Your output is 1-based, but the problem wants 0-based positions.\n- Remove the + 1", done_reason="length")
    assert OllamaClient().generate_feedback(context).text == "- Your output is 1-based, but the problem wants 0-based positions."


@pytest.mark.parametrize("text, expected", [
    # The last complete line or sentence ends in the second half: cut back to it.
    ("This first line of the hint is complete.\nLine two is cut mid", "This first line of the hint is complete."),
    ("The first sentence of this hint is done. Second one stops", "The first sentence of this hint is done."),
    ("Is the loop bound right for the last index? Then the cut", "Is the loop bound right for the last index?"),
    # Too early to cut back without losing most of the answer: keep it and mark the cut.
    ("Nothing complete here at all because the cap hit", "Nothing complete here at all because the cap hit…"),
    ("Short. Then a very long tail without any end that goes on and on and on and on", "Short. Then a very long tail without any end that goes on and on and on and on…"),
])
def test_trim_cut_off(text, expected):
    assert trim_cut_off(text) == expected
