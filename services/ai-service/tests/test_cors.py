from fastapi import FastAPI
from fastapi.testclient import TestClient

from app import api
from app.cors import allowed_origins, cors_middleware

FEEDBACK = "/submissions/dc7626b3-f4e7-4194-a625-300911712913/feedback"
FRONTEND = "http://localhost:5173"


def preflight(client, path, origin=FRONTEND, method="POST", headers="authorization,content-type"):
    return client.options(path, headers={
        "Origin": origin,
        "Access-Control-Request-Method": method,
        "Access-Control-Request-Headers": headers,
    })


def test_preflight_is_answered_for_the_feedback_endpoint():
    resp = preflight(TestClient(api.app), FEEDBACK)
    assert resp.status_code == 200
    assert resp.headers["access-control-allow-origin"] == "*"
    assert "POST" in resp.headers["access-control-allow-methods"]
    allowed = resp.headers["access-control-allow-headers"].lower()
    assert "authorization" in allowed and "content-type" in allowed
    assert resp.headers["access-control-max-age"] == "600"


def test_preflight_allows_x_user_id_for_local_development():
    resp = preflight(TestClient(api.app), "/health", method="GET", headers="x-user-id")
    assert resp.status_code == 200 and "x-user-id" in resp.headers["access-control-allow-headers"].lower()


def test_actual_response_carries_cors_headers_and_exposes_retry_after():
    resp = TestClient(api.app).get("/health", headers={"Origin": FRONTEND})
    assert resp.status_code == 200
    assert resp.headers["access-control-allow-origin"] == "*"
    assert "retry-after" in resp.headers["access-control-expose-headers"].lower()


def test_unlisted_method_is_refused():
    resp = preflight(TestClient(api.app), FEEDBACK, method="DELETE")
    assert resp.status_code == 400


def test_specific_origins_only():
    app = FastAPI(middleware=cors_middleware("https://judge.example.com, https://admin.example.com"))

    @app.get("/ping")
    def ping():
        return {"ok": True}

    client = TestClient(app)
    ok = preflight(client, "/ping", origin="https://judge.example.com", method="GET")
    assert ok.status_code == 200 and ok.headers["access-control-allow-origin"] == "https://judge.example.com"
    blocked = preflight(client, "/ping", origin="https://evil.example.com", method="GET")
    assert blocked.status_code == 400 and "access-control-allow-origin" not in blocked.headers


def test_origin_list_parsing():
    assert allowed_origins("*") == ["*"]
    assert allowed_origins(" https://a.com , ,https://b.com ") == ["https://a.com", "https://b.com"]
    assert allowed_origins("https://a.com,*") == ["*"]
