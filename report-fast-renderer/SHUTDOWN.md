# Graceful shutdown

The renderer registers persistent SIGTERM and SIGINT listeners before configuration or network startup. A signal received during startup remains pending; it prevents consumer intake from starting after startup finishes.

On shutdown the renderer stops RabbitMQ intake and starts HTTP graceful shutdown together. The vendored subscriber sends `basic.cancel` for its consumer tag, retains the actual spawned intake task, and joins every admitted callback and its awaited ack/nack/retry publication before closing the current connection. Cancellation and stream errors still wait for admitted work. Buffered deliveries that were not admitted to the worker set remain unacknowledged and are requeued when the connection closes. HTTP shutdown waits for admitted responses as well.

There is no forced task abort or fixed drain deadline. An unavailable broker or a stalled admitted operation may delay shutdown; operators must verify completion rather than infer it from a stop request. The existing renderer callbacks, queue settings, retry policy and HTTP routes are unchanged. These are source guarantees for a newly built artifact, not evidence that a previously running image has acquired signal handling.

Local checks (no broker, database or production requests):

```sh
cargo test --manifest-path report-fast-renderer/Cargo.toml --locked shutdown::tests
cargo test --manifest-path report-fast-renderer/vendor/cleanapp_rustlib/Cargo.toml --locked shutdown_tests
```

The process tests deliver real TERM/INT only to newly spawned test children while a simulated batch is unpolled. The subscriber tests hold callback and acknowledgment barriers independently and verify that shutdown and stream/cancellation failures cannot drop either.
