"""Stand-in for the real Catalog service, for local testing before the real
Catalog (Java/Spring Boot + Postgres) is wired up. Implements the same contract
(same path, same auth header, same JSON shape) as
services/catalog's InternalTestsetController, so the Judge Worker's actual HTTP
client code is genuinely exercised, not mocked away.

Run: python scripts/dev/fake_catalog.py
"""

import uvicorn
from fastapi import FastAPI, Header, HTTPException

app = FastAPI()

TESTSET = {
    "problem_id": "11111111-1111-1111-1111-111111111111",
    "version": 1,
    "content_hash": "fakehash123",
    "time_limit_ms": 2000,
    "memory_limit_mb": 128,
    "cases": [
        {"ordinal": 1, "input": "2 3\n", "expected_output": "5", "is_sample": True},
        {"ordinal": 2, "input": "10 20\n", "expected_output": "30", "is_sample": False},
    ],
}


@app.get("/internal/problems/{problem_id}/testset")
def testset(problem_id: str, x_service_token: str = Header(None)):
    if x_service_token != "local-dev-token":
        raise HTTPException(status_code=401, detail="unauthorized")
    return {**TESTSET, "problem_id": problem_id}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8080, log_level="warning")
