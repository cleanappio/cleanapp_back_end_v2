export AMQP_HOST="cleanapp_rabbitmq"
export AMQP_PORT="5672"
: "${AMQP_USER:?AMQP_USER is required (RabbitMQ username)}"
: "${AMQP_PASSWORD:?AMQP_PASSWORD is required (RabbitMQ password)}"
export AMQP_USER AMQP_PASSWORD
export RABBITMQ_EXCHANGE="cleanapp-exchange"
export RABBITMQ_RENDERER_QUEUE_NAME="report-renderer-queue"
export RABBITMQ_ANALYSED_REPORT_ROUTING_KEY="report.analysed"

cargo run --bin report-fast-renderer
