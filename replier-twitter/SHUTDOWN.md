# Graceful shutdown

The replier registers persistent SIGTERM and SIGINT listeners before parsing configuration or opening connections. A pending signal prevents consumer intake from beginning after startup.

After a signal, the vendored subscriber cancels further RabbitMQ deliveries and joins its actual spawned intake task. Each admitted callback runs its existing post-and-record operation to completion, followed by the existing awaited ack/nack/retry handling. Only then does the subscriber close the current RabbitMQ connection; the service also awaits database-pool disconnection before returning success. A cancellation or stream error still waits for admitted work. Buffered deliveries that were not admitted to the worker set remain unacknowledged and requeue on connection closure.

No new reply, synthetic message, queue policy, external notification or business operation is added by shutdown. There is no forced cancellation or fixed drain deadline, and no claim that a previously running image has this behavior. Broker or callback failures can delay shutdown or produce an error; operators must verify the completed drain.

Local checks (no broker, database or external reply):

```sh
cargo test --manifest-path replier-twitter/Cargo.toml --locked shutdown::tests
cargo test --manifest-path replier-twitter/vendor/cleanapp_rustlib/Cargo.toml --locked shutdown_tests
```

Signal fixtures target only freshly spawned test processes. Drain regressions retain both callback and acknowledgment futures while shutdown, consumer errors or cancellation errors are observed.
