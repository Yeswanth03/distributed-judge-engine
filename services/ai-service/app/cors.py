"""CORS: lets a browser frontend on another origin call the AI service.

The gateway passes browser preflights (OPTIONS) through without checking a token
(deploy/gateway/envoy.yaml, bypass_cors_preflight) and expects each service to
answer them itself, as the Submission and User services already do.

Origins come from AI_CORS_ORIGINS: "*" (the local default, like CORS_ORIGIN on the
other services) or a comma-separated list such as "https://judge.example.com".
Credentials (cookies) are not allowed: the frontend sends its access token in the
Authorization header, which works with any origin setting.
"""

from fastapi.middleware.cors import CORSMiddleware
from starlette.middleware import Middleware

from .config import settings

ALLOWED_METHODS = ["GET", "POST", "OPTIONS"]
# Authorization is what browsers send through the gateway; X-User-Id is only for
# local development without the gateway (the gateway replaces it with the real one).
ALLOWED_HEADERS = ["Authorization", "Content-Type", "X-User-Id", "X-Request-Id"]
# Retry-After lets the frontend wait the right time after a 503 circuit_open.
EXPOSED_HEADERS = ["Retry-After", "X-Request-Id"]


def allowed_origins(raw: str | None = None) -> list[str]:
    value = settings.cors_origins if raw is None else raw
    origins = [origin.strip() for origin in value.split(",") if origin.strip()]
    return ["*"] if "*" in origins else origins


def cors_middleware(raw_origins: str | None = None) -> list[Middleware]:
    return [
        Middleware(
            CORSMiddleware,
            allow_origins=allowed_origins(raw_origins),
            allow_methods=ALLOWED_METHODS,
            allow_headers=ALLOWED_HEADERS,
            expose_headers=EXPOSED_HEADERS,
            allow_credentials=False,
            max_age=600,  # browsers may cache a preflight answer for 10 minutes
        )
    ]
