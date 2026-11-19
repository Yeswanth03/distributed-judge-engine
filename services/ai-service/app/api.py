import logging
import time
from contextlib import asynccontextmanager
from threading import Thread
from uuid import UUID

import httpx
from fastapi import FastAPI, Header, HTTPException, Request, Response, status
from fastapi.responses import JSONResponse

from . import metrics
from .broker import consume_forever
from .clients import ContextNotFound
from .cors import cors_middleware
from .database import feedback_database
from .feedback import NotAccepted, SubmissionNotJudged, feedback_processor
from .resilience import CircuitOpenError, breakers
from .schemas import FeedbackResponse

logging.basicConfig(level=logging.INFO)

# The gateway verifies the login token and forwards the user as X-User-Id
# (docs/contracts.md). This service must only be reachable through it.
UserId = Header(..., alias="X-User-Id", min_length=1, max_length=200)


@asynccontextmanager
async def lifespan(_app: FastAPI):
    feedback_database.initialize()
    feedback_processor.start()
    Thread(
        target=consume_forever,
        args=(feedback_processor.process_failed_verdict,),
        name="failed-verdict-consumer",
        daemon=True,
    ).start()
    yield
    feedback_processor.stop()


app = FastAPI(
    middleware=cors_middleware(),
    title="AI Feedback Service",
    description=(
        "Feedback on code submissions. Source code comes from the Submission Service and the "
        "problem statement and test cases from Catalog; only sample cases are ever shown to the model."
    ),
    version="0.3.0",
    lifespan=lifespan,
)

for _breaker in breakers.values():
    metrics.track_breaker(_breaker)


@app.middleware("http")
async def record_http_metrics(request: Request, call_next):
    """Request count and latency per route template (/submissions/{submission_id}/feedback),
    never per concrete URL, so IDs don't turn into separate time series."""
    started = time.perf_counter()
    status_code = 500
    try:
        response = await call_next(request)
        status_code = response.status_code
        return response
    finally:
        route = request.scope.get("route")
        path = getattr(route, "path", "unmatched")
        if path != "/metrics":
            metrics.HTTP_REQUESTS.labels(request.method, path, str(status_code)).inc()
            metrics.HTTP_LATENCY.labels(request.method, path).observe(time.perf_counter() - started)


@app.post(
    "/submissions/{submission_id}/feedback",
    response_model=FeedbackResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Request AI feedback for a judged submission",
    responses={
        200: {"model": FeedbackResponse, "description": "Feedback for this submission is already ready."},
        202: {"model": FeedbackResponse, "description": "Queued or processing; poll GET /feedback-requests/{request_id}."},
        404: {"description": "No such submission for this user."},
        409: {"description": "The submission has not been judged yet."},
        502: {"description": "The Submission Service could not be reached."},
        503: {"description": "The Submission Service's circuit is open; retry after `retry_after` seconds."},
    },
)
def request_feedback(submission_id: UUID, response: Response, x_user_id: str = UserId) -> dict:
    """Works for any final verdict, accepted included. Returns the existing request
    when one is queued, processing or ready for this submission (including the
    automatic one made for a failed verdict); asks again only after 'unavailable'."""
    try:
        row, _created = feedback_processor.request_on_demand(submission_id, x_user_id)
    except ContextNotFound as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Submission not found") from exc
    except SubmissionNotJudged as exc:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=str(exc)) from exc
    except CircuitOpenError as exc:
        return circuit_open_response(exc)
    except httpx.HTTPError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail="The Submission Service is unavailable; try again shortly.",
        ) from exc

    if row["status"] == "ready":
        response.status_code = status.HTTP_200_OK
    return row


@app.post(
    "/submissions/{submission_id}/feedback/direct",
    response_model=FeedbackResponse,
    status_code=status.HTTP_200_OK,
    summary="Get AI hints now, for an accepted submission",
    responses={
        200: {"model": FeedbackResponse, "description": "Hints generated in this request (or saved earlier)."},
        202: {"model": FeedbackResponse, "description": "The model was too slow or unavailable; generation continues "
                                                         "in the background. Poll GET /feedback-requests/{request_id}."},
        404: {"description": "No such submission for this user."},
        409: {"description": "The submission is not accepted; use POST /submissions/{submission_id}/feedback."},
        502: {"description": "The Submission Service could not be reached."},
        503: {"description": "The Submission Service's circuit is open; retry after `retry_after` seconds."},
    },
)
def request_direct_feedback(submission_id: UUID, response: Response, x_user_id: str = UserId) -> dict:
    """For accepted submissions only: calls the model right away and returns the hints in the
    response, instead of queueing them. Failed submissions keep the queued endpoint above,
    unchanged. If the model can't answer within AI_DIRECT_TIMEOUT_SECONDS (under the gateway's
    15 s limit), the request continues in the background and this answers 202."""
    try:
        row, ready = feedback_processor.request_direct(submission_id, x_user_id)
    except ContextNotFound as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Submission not found") from exc
    except NotAccepted as exc:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Direct hints are only for accepted submissions; use POST /submissions/{id}/feedback",
        ) from exc
    except CircuitOpenError as exc:
        return circuit_open_response(exc)
    except httpx.HTTPError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail="The Submission Service is unavailable; try again shortly.",
        ) from exc

    if not ready:
        response.status_code = status.HTTP_202_ACCEPTED
    return row


@app.get(
    "/feedback-requests/{request_id}",
    response_model=FeedbackResponse,
    summary="Get feedback request status and result",
    responses={404: {"description": "Request not found for this user."}},
)
def get_feedback_request(request_id: UUID, x_user_id: str = UserId) -> dict:
    row = feedback_database.get_request(request_id, x_user_id)
    if row is None:
        raise HTTPException(status_code=404, detail="Feedback request not found")
    return row


@app.get(
    "/submissions/{submission_id}/feedback",
    response_model=FeedbackResponse,
    summary="Get the latest feedback request for a submission",
    responses={404: {"description": "No feedback exists for this submission and user."}},
)
def get_submission_feedback(submission_id: UUID, x_user_id: str = UserId) -> dict:
    row = feedback_database.get_latest_for_submission(submission_id, x_user_id)
    if row is None:
        raise HTTPException(status_code=404, detail="No feedback for this submission")
    return row


def circuit_open_response(exc: CircuitOpenError) -> JSONResponse:
    """Fail fast while a dependency's circuit is open, telling the client when to retry."""
    retry_after = max(1, round(exc.retry_after))
    return JSONResponse(
        status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
        headers={"Retry-After": str(retry_after)},
        content={
            "status": "circuit_open",
            "dependency": exc.dependency,
            "retry_after": retry_after,
            "message": f"{exc.dependency} is temporarily unavailable",
        },
    )


@app.get("/metrics", include_in_schema=False)
def prometheus_metrics() -> Response:
    """Scraped by Prometheus every few seconds (plain-text exposition format)."""
    body, content_type = metrics.render()
    return Response(content=body, media_type=content_type)


@app.get("/health", summary="Liveness probe, with each dependency's circuit state")
def health() -> dict:
    """Always 200 while the process is up: an open circuit is degraded service, not a dead one."""
    circuits = {name: breaker.snapshot() for name, breaker in breakers.items()}
    degraded = any(c["state"] != "closed" for c in circuits.values())
    return {"status": "degraded" if degraded else "ok", "circuits": circuits}
