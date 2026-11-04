def main() -> None:
    import uvicorn

    from .config import settings

    uvicorn.run("app.api:app", host=settings.api_host, port=settings.api_port)


if __name__ == "__main__":
    main()
