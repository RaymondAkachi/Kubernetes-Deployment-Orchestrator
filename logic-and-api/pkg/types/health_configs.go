package types

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type HealthCheckConfig struct {
	Enabled bool `json:"enabled"`

	// External orchestrator-level checks (e.g., Prometheus)
	// You can add these fields here if needed.
	PrometheusQueries []PrometheusMetricQuery `json:"prometheusQueries,omitempty"`
    Timeout                 time.Duration           `json:"timeout" validate:"min=1s"`
    RetryInterval           time.Duration           `json:"retryInterval" validate:"min=1s"`
    MaxRetries              int                     `json:"maxRetries" validate:"min=0"`
}

// ProbeConfig defines the parameters for a single Kubernetes probe.
type ProbeConfig struct {
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int32 `json:"periodSeconds,omitempty"`
	TimeoutSeconds      int32 `json:"timeoutSeconds,omitempty"`
	SuccessThreshold    int32 `json:"successThreshold,omitempty"`
	FailureThreshold    int32 `json:"failureThreshold,omitempty"`

	// These fields are mutually exclusive. Exactly one must be provided.
	HTTPGet   *HTTPGetAction   `json:"httpGet,omitempty" validate:"required_without_all=TCPSocket Exec"`
	TCPSocket *TCPSocketAction `json:"tcpSocket,omitempty" validate:"required_without_all=HTTPGet Exec"`
	Exec      *ExecAction      `json:"exec,omitempty" validate:"required_without_all=HTTPGet TCPSocket"`
}

// HTTPGetAction defines an HTTP GET health check.
type HTTPGetAction struct {
	Path string `json:"path,omitempty"`
	Port int32  `json:"port,omitempty" validate:"required"`
}

// TCPSocketAction defines a TCP socket health check.
type TCPSocketAction struct {
	Port int32 `json:"port,omitempty" validate:"required"`
}

// ExecAction defines an exec health check.
type ExecAction struct {
	Command []string `json:"command,omitempty" validate:"required,min=1"`
}

// PrometheusMetricQuery defines a Prometheus query for health checks
type PrometheusMetricQuery struct {
	Name        string  `json:"name" validate:"required"`
	Query       string  `json:"query" validate:"required"`
	Threshold   float64 `json:"threshold" validate:"required"`
	Operator    string  `json:"operator" validate:"required,oneof=gt gte lt lte eq ne"`
	Description string  `json:"description,omitempty"`
}

// NewKubeProbe converts a ProbeConfig to a Kubernetes corev1.Probe object.
func NewKubeProbe(probeConfig *ProbeConfig) *corev1.Probe {
	if probeConfig == nil {
		return nil
	}
	
	kubeProbe := &corev1.Probe{
		InitialDelaySeconds: probeConfig.InitialDelaySeconds,
		PeriodSeconds:       probeConfig.PeriodSeconds,
		TimeoutSeconds:      probeConfig.TimeoutSeconds,
		SuccessThreshold:    probeConfig.SuccessThreshold,
		FailureThreshold:    probeConfig.FailureThreshold,
	}

	// Determine which probe type was provided and set the appropriate handler.
	if probeConfig.HTTPGet != nil {
		kubeProbe.ProbeHandler = corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: probeConfig.HTTPGet.Path,
				Port: intstr.FromInt(int(probeConfig.HTTPGet.Port)),
			},
		}
	} else if probeConfig.TCPSocket != nil {
		kubeProbe.ProbeHandler = corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(int(probeConfig.TCPSocket.Port)),
			},
		}
	} else if probeConfig.Exec != nil {
		kubeProbe.ProbeHandler = corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: probeConfig.Exec.Command,
			},
		}
	} else {
		return nil
	}

	return kubeProbe
}