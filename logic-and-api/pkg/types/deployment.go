// pkg/types/deployment.go
package types

import (
	"time"
)

// DeploymentCreateRequest represents the request for creating a new deployment
type DeploymentCreateRequest struct {
	Namespace         string             `json:"namespace" validate:"required,min=3"`
	Name              string             `json:"name" validate:"required,min=3"`
	Replicas          int32              `json:"replicas" validate:"required,min=1"`
	Strategy          string             `json:"strategy" validate:"required,oneof=blue-green canary ab"`
	ContainerSpec    *ContainerSpec      `json:"container_spec" validate:"required"`
	HealthCheckConfig *HealthCheckConfig `json:"healthCheckConfig,omitempty"`

	// Conditional validation: at least one of these must be provided if strategy is specified
	BlueGreenConfig *BlueGreenConfig `json:"blueGreenConfig,omitempty" validate:"required_if=Strategy blue-green"`
	CanaryConfig    *CanaryConfig    `json:"canaryConfig,omitempty" validate:"required_if=Strategy canary"`
	ABConfig        *ABConfig        `json:"abConfig,omitempty" validate:"required_if=Strategy ab"`
}

// DeploymentUpdateRequest represents the request for updating an existing deployment
type DeploymentUpdateRequest struct {
	Namespace         string             `json:"namespace" validate:"required,min=3"`
	Name              string             `json:"name" validate:"required,min=3"`
	NewReplicas          int32           `json:"replicas,omitempty" validate:"omitempty,min=1"`
	NewContainerSpec    *ContainerSpec   `json:"new_container_spec" validate:"required"`
	NewHealthCheckConfig *HealthCheckConfig `json:"new_healthCheck_config,omitempty"`

	// Note: These configs are now optional since the strategy is already defined in the original deployment
	NewABConfig          *ABConfig       `json:"new_abConfig,omitempty" validate:"omitempty"`
	NewCanaryConfig    *CanaryConfig   `json:"new_canaryConfig,omitempty" validate:"omitempty"`
	NewBlueGreenConfig   *BlueGreenConfig`json:"new_blueGreenConfig,omitempty" validate:"omitempty"`
}


type ContainerSpec struct {
	Image   string         `json:"image" validate:"required"`
	Port   int32           `json:"ports" validate:"required"`
	CPU     string         `json:"cpu" validate:"required"`
	Memory  string         `json:"memory" validate:"required"`

	LivenessProbe  *ProbeConfig `json:"livenessProbe,omitempty"`
	ReadinessProbe *ProbeConfig `json:"readinessProbe,omitempty"`
}

type ServiceConfig struct {
	Name         string `json:"name" validate:"required"`
	Port         int32  `json:"port" validate:"required"`
	Type         string `json:"type" validate:"required"`// ClusterIP, NodePort, LoadBalancer, ExternalName
	ExternalName string `json:"external_name,omitempty" validate:"required_if=Type ExternalName"`
}

// DeploymentStatus represents the current state of a deployment
type DeploymentStatus struct {
	ID           string                 `json:"id"`
	Namespace    string                 `json:"namespace"`
	Name         string                 `json:"name"`
	Status       string                 `json:"status"` // e.g., "pending", "in-progress", "success", "failed", "rolled-back"
	CurrentPhase string                 `json:"currentPhase"`
	Error        string                 `json:"error,omitempty"`
	StartTime    time.Time              `json:"startTime"`
	EndTime      *time.Time             `json:"endTime,omitempty"`
	Metadata     map[string]interface{} `json:"metadata"` // e.g., current_traffic_percent, active_deployment
	Events       []DeploymentEvent      `json:"events"`
}

type GetDeploymentStatus struct {
	Namespace  string  `json:"namespace" validate:"required"`
	Name       string  	`json:"name" validate:"required"`
}

// DeploymentEvent represents an event during deployment
type DeploymentEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Phase     string    `json:"phase"`
	Message   string    `json:"message"`
	Level     string    `json:"level"` // e.g., "info", "error"
}

// RollbackRequest represents a request to roll back a deployment
type RollbackRequest struct {
	Namespace string `json:"namespace" validate:"required"`
	Name      string `json:"name" validate:"required"`
	Reason    string `json:"reason,omitempty"`
}

// ListDeploymentsRequest represents a request to list deployments
type ListDeploymentsRequest struct {
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status,omitempty"`
	Strategy  string `json:"strategy,omitempty"`
	Limit     int    `json:"limit,omitempty" validate:"min=0"`
	// Offset    int    `json:"offset,omitempty" validate:"min=0"`
}

// ListDeploymentsResponse represents the response for listing deployments
type ListDeploymentsResponse struct {
	Deployments []DeploymentStatus `json:"deployments"`
	Total       int                `json:"total"`
	Limit       int                `json:"limit"`
	Offset      int                `json:"offset"`
}

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	Healthy     bool              `json:"healthy"`
	Message     string            `json:"message"`
	Details     map[string]string `json:"details,omitempty"`
	Metrics     []MetricResult    `json:"metrics,omitempty"`
	CheckedAt   time.Time         `json:"checkedAt"`
}

// MetricResult represents a metric check result
type MetricResult struct {
	Name        string  `json:"name"`
	Value       float64 `json:"value"`
	Threshold   float64 `json:"threshold"`
	Operator    string  `json:"operator"`
	Passed      bool    `json:"passed"`
	Description string  `json:"description,omitempty"`
}