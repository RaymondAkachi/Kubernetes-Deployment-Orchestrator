package monitoring

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	DeploymentsTotal      *prometheus.CounterVec
	DeploymentDuration    *prometheus.HistogramVec
	DeploymentErrors      *prometheus.CounterVec
	RollbacksTotal        *prometheus.CounterVec
	ActiveDeployments     *prometheus.GaugeVec
	HealthCheckDuration   *prometheus.HistogramVec
	HealthCheckFailures   *prometheus.CounterVec
	TrafficSwitchDuration *prometheus.HistogramVec
}

// Package-level metrics variable
var metrics *Metrics


// This will be called at application startup in the application main.go file
func InitMetrics() {
    metrics = NewMetrics()
}

func NewMetrics() *Metrics {
	return &Metrics{
		DeploymentsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orchestrator_deployments_total",
				Help: "Total number of deployments initiated",
			},
			[]string{"namespace", "deployment"},
		),
		DeploymentDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "orchestrator_deployment_duration_seconds",
				Help:    "Time taken to complete deployments",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"namespace", "deployment"},
		),
		DeploymentErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orchestrator_deployment_errors_total",
				Help: "Total number of deployment errors",
			},
			[]string{"namespace", "deployment", "error_type"},
		),
		RollbacksTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orchestrator_rollbacks_total",
				Help: "Total number of rollbacks executed",
			},
			[]string{"namespace", "deployment"},
		),
		ActiveDeployments: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "orchestrator_active_deployments",
				Help: "Number of currently active deployments",
			},
			[]string{"namespace"},
		),
		HealthCheckDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "orchestrator_health_check_duration_seconds",
				Help:    "Time taken to complete health checks",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
			},
			[]string{"namespace", "deployment", "check_type"},
		),
		HealthCheckFailures: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "orchestrator_health_check_failures_total",
				Help: "Total number of health check failures",
			},
			[]string{"namespace", "deployment", "check_type"},
		),
		TrafficSwitchDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "orchestrator_traffic_switch_duration_seconds",
				Help:    "Time taken to switch traffic during deployments",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
			},
			[]string{"namespace", "deployment"},
		),
	}
}

func (m *Metrics) RecordDeployment(namespace, deployment string) {
	m.DeploymentsTotal.WithLabelValues(namespace, deployment).Inc()
}

func (m *Metrics) RecordDeploymentDuration(namespace, deployment string, duration float64) {
	m.DeploymentDuration.WithLabelValues(namespace, deployment).Observe(duration)
}

func (m *Metrics) RecordDeploymentError(namespace, deployment, errorType string) {
	m.DeploymentErrors.WithLabelValues(namespace, deployment, errorType).Inc()
}

func (m *Metrics) RecordRollback(namespace, deployment string) {
	m.RollbacksTotal.WithLabelValues(namespace, deployment).Inc()
}

func (m *Metrics) SetActiveDeployments(namespace string, count float64) {
	m.ActiveDeployments.WithLabelValues(namespace).Set(count)
}

func (m *Metrics) RecordHealthCheckDuration(namespace, deployment, checkType string, duration float64) {
	m.HealthCheckDuration.WithLabelValues(namespace, deployment, checkType).Observe(duration)
}

func (m *Metrics) RecordHealthCheckFailure(namespace, deployment, checkType string) {
	m.HealthCheckFailures.WithLabelValues(namespace, deployment, checkType).Inc()
}

func (m *Metrics) RecordTrafficSwitchDuration(namespace, deployment string, duration float64) {
	m.TrafficSwitchDuration.WithLabelValues(namespace, deployment).Observe(duration)
}
