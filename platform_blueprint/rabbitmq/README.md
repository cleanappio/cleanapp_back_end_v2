# RabbitMQ Reliability (Prod)

This folder contains **prod-safe**, **secrets-safe** tooling for hardening the RabbitMQ event pipeline.

## What We Enforce

1. **Dead-lettering** for poison/permanent failures:
   - Each primary queue gets a `*.dlq` companion queue.
   - Primary queues have a policy that sets:
     - `dead-letter-exchange` to `cleanapp-dlx`
     - `dead-letter-routing-key` to `<queue>.dlq`
2. **No secrets in git**:
   - Scripts do not require existing RabbitMQ credentials.
   - When the management API must be used (to create exchanges/queues/bindings), we create a short-lived admin user via `rabbitmqctl`, use it, then delete it.

## Apply On Prod VM

Run on the prod VM (as a user that can `sudo docker exec`):

```bash
./platform_blueprint/rabbitmq/apply_prod_dlx_dlq.sh
```

To check status:

```bash
./platform_blueprint/rabbitmq/check_status.sh
```

## Notes

- If a queue has no DLQ policy configured, `Nack(requeue=false)` will **drop** messages (unless the queue has DLX args).
- With this DLQ setup, permanent failures land in DLQs for inspection instead of being silently discarded.

