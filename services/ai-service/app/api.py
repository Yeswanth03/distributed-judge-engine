import logging
from contextlib import asynccontextmanager
from threading import Thread
from uuid import UUID

import httpx
from fastapi import FastAPI, Header, HTTPException, Response, status
from fastapi.responses import JSONResponse

from .broker import consume_forever
from .clients import ContextNotFound
from .database import feedback_database
from .feedback import SubmissionNotJudged, feedback_processor
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
    title="AI Feedback Service",
    description=(
        "Feedback on code submissions. Source code comes from the Submission Service and the "
        "problem statement and test cases from Catalog; only sample cases are ever shown to the model."
    ),
    version="0.3.0",
    lifespan=lifespan,
)


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


@app.get("/health", summary="Liveness probe, with each dependency's circuit state")
def health() -> dict:
    """Always 200 while the process is up: an open circuit is degraded service, not a dead one."""
    circuits = {name: breaker.snapshot() for name, breaker in breakers.items()}
    degraded = any(c["state"] != "closed" for c in circuits.values())
    return {"status": "degraded" if degraded else "ok", "circuits": circuits}
