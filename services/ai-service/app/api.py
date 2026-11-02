from contextlib import asynccontextmanager
from threading import Thread
from uuid import UUID

from fastapi import FastAPI, Header, HTTPException, Response, status

from .broker import consume_forever
from .database import feedback_database
from .feedback import feedback_processor
from .schemas import FeedbackResponse


@asynccontextmanager
async def lifespan(_app: FastAPI):
    feedback_database.initialize()
    worker = Thread(
        target=consume_forever,
        args=(feedback_processor.process_failed_verdict,),
        name="failed-verdict-consumer",
        daemon=True,
    )
    worker.start()
    yield


app = FastAPI(
    title="AI Feedback Service",
    description="Request and retrieve feedback generated for code submissions.",
    version="0.2.0",
    lifespan=lifespan,
)


@app.post(
    "/submissions/{submission_id}/feedback",
    response_model=FeedbackResponse,
    status_code=status.HTTP_200_OK,
    summary="Request AI feedback",
    responses={
        202: {"model": FeedbackResponse, "description": "Feedback generation is still in progress."},
        503: {"model": FeedbackResponse, "description": "Feedback generation is unavailable."},
    },
)
def request_feedback(
    submission_id: UUID,
    response: Response,
    x_user_id: str = Header(..., alias="X-User-ID", min_length=1, max_length=200),
    idempotency_key: str = Header(..., alias="Idempotency-Key", min_length=1, max_length=200),
) -> dict:
    """Generate and store feedback. Local mock accepts any submission ID."""
    try:
        row = feedback_processor.request_feedback(
            submission_id=submission_id,
            user_id=x_user_id,
            verdict="accepted",
            rejudge_seq=0,
            trigger="user_request",
            idempotency_key=f"user:{submission_id}:{idempotency_key}",
        )
    except Exception as exc:
        failed = feedback_database.get_request_by_key(
            f"user:{submission_id}:{idempotency_key}", x_user_id
        )
        if failed is None:
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail="Feedback generation failed before the request could be saved.",
            ) from exc
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return failed

    if row["status"] != "ready":
        response.status_code = status.HTTP_202_ACCEPTED
    return row


@app.get(
    "/feedback-requests/{request_id}",
    response_model=FeedbackResponse,
    summary="Get feedback request status and result",
    responses={404: {"description": "Request not found for this user."}},
)
def get_feedback_request(
    request_id: UUID,
    x_user_id: str = Header(..., alias="X-User-ID", min_length=1, max_length=200),
) -> dict:
    row = feedback_database.get_request(request_id, x_user_id)
    if row is None:
        raise HTTPException(status_code=404, detail="Feedback request not found")
    return row


@app.get(
    "/submissions/{submission_id}/feedback",
    response_model=FeedbackResponse,
    summary="Get the latest saved feedback for a submission",
    responses={404: {"description": "No feedback exists for this submission and user."}},
)
def get_submission_feedback(
    submission_id: UUID,
    x_user_id: str = Header(..., alias="X-User-ID", min_length=1, max_length=200),
) -> dict:
    row = feedback_database.get_latest_for_submission(submission_id, x_user_id)
    if row is None:
        raise HTTPException(status_code=404, detail="No saved feedback for this submission")
    return row
