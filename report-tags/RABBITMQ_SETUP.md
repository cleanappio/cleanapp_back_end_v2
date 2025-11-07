# RabbitMQ Setup for Report Tags Service

## Current Status

The service gracefully handles RabbitMQ connection failures. If RabbitMQ is unavailable, you'll see a warning but the service continues to work normally. Tag events simply won't be published to RabbitMQ.

## Setup Options

### Option 1: Docker Compose (Recommended)

1. Start Docker Desktop (if not already running)

2. Start RabbitMQ:
   ```bash
   cd report-tags
   docker compose up -d rabbitmq
   ```

3. Verify RabbitMQ is running:
   ```bash
   docker compose ps
   ```

4. Access Management UI (optional):
   - URL: http://localhost:15672
   - Username: `guest`
   - Password: `guest`

5. Stop RabbitMQ:
   ```bash
   docker compose down
   ```

### Option 2: Local Installation (macOS)

1. Install RabbitMQ:
   ```bash
   brew install rabbitmq
   ```

2. Start RabbitMQ:
   ```bash
   brew services start rabbitmq
   ```

3. Verify it's running:
   ```bash
   rabbitmqctl status
   ```

### Option 3: Run Without RabbitMQ

The service works fine without RabbitMQ. It will log a warning on startup but continue normally. Tag events just won't be published to the message queue.

## Environment Variables

The `.env` file should contain:

```bash
AMQP_HOST=localhost          # Use 'rabbitmq' if running in Docker Compose
AMQP_PORT=5672
AMQP_USER=guest
AMQP_PASSWORD=guest
RABBITMQ_EXCHANGE=cleanapp
RABBITMQ_QUEUE=report-tags
RABBITMQ_RAW_REPORT_ROUTING_KEY=report.raw
RABBITMQ_TAG_EVENT_ROUTING_KEY=tag.added
```

## Verify Connection

After starting RabbitMQ, restart the report-tags service. You should see:
```
INFO - RabbitMQ publisher initialized successfully
```

Instead of:
```
WARN - Failed to initialize RabbitMQ publisher: ... Continuing without RabbitMQ.
```

## Troubleshooting

- **Connection refused**: RabbitMQ is not running. Start it using one of the options above.
- **Authentication failed**: Check `AMQP_USER` and `AMQP_PASSWORD` in `.env`
- **Wrong host**: If using Docker Compose, use `AMQP_HOST=rabbitmq` instead of `localhost`

