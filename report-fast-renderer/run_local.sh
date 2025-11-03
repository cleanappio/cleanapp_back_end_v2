export AMQP_HOST="cleanapp_rabbitmq"
export AMQP_PORT="5672"
export AMQP_USER="cleanapp"
export AMQP_PASSWORD="cleanapp"
export RABBITMQ_EXCHANGE="cleanapp-exchange"
export RABBITMQ_RENDERER_QUEUE_NAME="report-renderer-queue"
export RABBITMQ_ANALYSED_REPORT_ROUTING_KEY="report.analysed"

cargo run --bin report-fast-renderer