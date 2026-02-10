package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	once sync.Once

	// RabbitMQConnected is 1 when the subscriber considers itself connected.
	RabbitMQConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_connected",
		Help:      "Whether the analyzer RabbitMQ subscriber is currently connected (best-effort).",
	})

	// RabbitMQLastConnectSeconds is a unix timestamp (seconds) of last successful connect.
	RabbitMQLastConnectSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_last_connect_timestamp_seconds",
		Help:      "Unix timestamp (seconds) of the last successful RabbitMQ connect (best-effort).",
	})

	// RabbitMQLastDeliverySeconds is a unix timestamp (seconds) of last observed delivery.
	RabbitMQLastDeliverySeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_last_delivery_timestamp_seconds",
		Help:      "Unix timestamp (seconds) of the last RabbitMQ delivery observed by the subscriber (best-effort).",
	})

	// WorkerInFlight is the current number of deliveries being processed by workers.
	WorkerInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_worker_in_flight",
		Help:      "Current number of RabbitMQ deliveries being processed by worker goroutines.",
	})

	// ProcessedTotal counts processed deliveries by outcome.
	ProcessedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_processed_total",
		Help:      "Total number of RabbitMQ deliveries processed by the analyzer subscriber, labeled by result.",
	}, []string{"result"})

	// ProcessingDurationSeconds is end-to-end time per delivery, measured inside the worker.
	ProcessingDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_processing_duration_seconds",
		Help:      "End-to-end time to process a RabbitMQ delivery (callback + ack/nack).",
		// Keep buckets fairly coarse to avoid high-cardinality time series.
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 60, 120, 300},
	}, []string{"result"})

	AckErrorTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_ack_error_total",
		Help:      "Total number of RabbitMQ ack errors.",
	})

	NackErrorTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_nack_error_total",
		Help:      "Total number of RabbitMQ nack errors.",
	})

	RetryPublishErrorTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cleanapp",
		Subsystem: "analyzer",
		Name:      "rabbitmq_retry_publish_error_total",
		Help:      "Total number of retry-exchange publish errors.",
	})
)

// Register registers analyzer metrics with the default Prometheus registry.
// Safe to call multiple times.
func Register() {
	once.Do(func() {
		prometheus.MustRegister(
			RabbitMQConnected,
			RabbitMQLastConnectSeconds,
			RabbitMQLastDeliverySeconds,
			WorkerInFlight,
			ProcessedTotal,
			ProcessingDurationSeconds,
			AckErrorTotal,
			NackErrorTotal,
			RetryPublishErrorTotal,
		)
	})
}

func NowUnixSeconds() float64 {
	return float64(time.Now().Unix())
}
