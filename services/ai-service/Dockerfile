FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
COPY db ./db

EXPOSE 8084
CMD ["python", "-m", "app.main"]
