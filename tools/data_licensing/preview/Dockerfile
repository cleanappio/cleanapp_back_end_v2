FROM python:3.14.3-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt && useradd --uid 10001 --create-home preview
COPY --chown=10001:10001 server.py ./
COPY --chown=10001:10001 static/ ./static/
USER 10001:10001
EXPOSE 8000
CMD ["uvicorn", "server:create_from_env", "--factory", "--host", "0.0.0.0", "--port", "8000", "--no-access-log", "--no-proxy-headers"]
