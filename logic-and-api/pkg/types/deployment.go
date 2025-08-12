// pkg/types/deployment.go
package types

import (
	"context"
	"time"
)

// DeploymentCreateRequest represents the request for creating a new deployment
type DeploymentCreateRequest struct {
	Namespace         string            `json:"namespace" validate:"required"`
	Name              string            `json:"name" validate:"required"`
	Image             string            `json:"image" validate:"required"`
	Replicas          int32               `json:"replicas" validate:"required,min=1"`
	Strategy          string            `json:"strategy" validate:"required"`
	BlueGreenConfig   *BlueGreenConfig  `json:"blueGreenConfig,omitempty" validate:"required_if=Strategy blue-green"`
	CanaryConfig      *CanaryConfig     `json:"canaryConfig,omitempty" validate:"required_if=Strategy canary"`
	ABConfig          *ABConfig         `json:"abConfig,omitempty" validate:"required_if=Strategy ab"`
	FeatureFlagConfig *FeatureFlagConfig `json:"featureFlagConfig,omitempty" validate:"required_if=Strategy feature-flag"`
	HealthCheckConfig *HealthCheckConfig `json:"healthCheckConfig,omitempty"`
}

// DeploymentUpdateRequest represents the request for updating an existing deployment
type DeploymentUpdateRequest struct {
	Namespace         string            `json:"namespace" validate:"required"`
	Name              string            `json:"name" validate:"required"`
	NewImage          string            `json:"newImage" validate:"required"`
	Replicas          int32               `json:"replicas,omitempty" validate:"omitempty,min=1"`
	StrategyOverride  *StrategyOverride `json:"strategyOverride,omitempty"`
	HealthCheckConfig *HealthCheckConfig `json:"healthCheckConfig,omitempty"`
	CanaryConfig      *CanaryConfig     `json:"canaryConfig,omitempty"`
	ABConfig          *ABConfig         `json:"abConfig,omitempty" validate:"required_if=StrategyOverride.Strategy ab"`
	FeatureFlagConfig *FeatureFlagConfig `json:"featureFlagConfig,omitempty" validate:"required_if=StrategyOverride.Strategy feature-flag"`
}

// StrategyOverride allows overriding the strategy for updates
type StrategyOverride struct {
	Strategy        string           `json:"strategy" validate:"required,oneof=blue-green canary"`
	BlueGreenConfig *BlueGreenConfig `json:"blueGreenConfig,omitempty" validate:"required_if=Strategy blue-green"`
	CanaryConfig    *CanaryConfig    `json:"canaryConfig,omitempty" validate:"required_if=Strategy canary"`
	ABConfig          *ABConfig         `json:"abConfig,omitempty" validate:"required_if=Strategy ab"`
	FeatureFlagConfig *FeatureFlagConfig `json:"featureFlagConfig,omitempty" validate:"required_if=Strategy feature-flag"`
}

// BlueGreenConfig defines configuration for blue-green deployments
type BlueGreenConfig struct {
	ServiceName       string `json:"serviceName" validate:"required"`
	ActiveEnvironment string `json:"activeEnvironment" validate:"required,oneof=blue green"`
	HealthCheck       string `json:"healthCheck,omitempty"`
}

// CanaryConfig defines configuration for canary deployments
type CanaryConfig struct {
    TrafficIncrement      int           `json:"traffic_increment" yaml:"traffic_increment"`
    StepDuration          time.Duration `json:"step_duration" yaml:"step_duration"`
	InitialTrafficPercent int       `json:"initialTrafficPercent" validate:"required,min=1,max=100"`
	// TrafficSteps      []TrafficStep `json:"trafficSteps" validate:"required"`
	MaxTrafficPercent int           `json:"maxTrafficPercent" validate:"required,min=1,max=100"`
	ServiceName       string        `json:"serviceName" validate:"required"`
	AutoPromote       bool          `json:"autoPromote" validate:"required"`
	AnalysisInterval  time.Duration `json:"analysis_interval" validate:"required"`
}

// TrafficStep represents a step in traffic shifting for canary
type TrafficStep struct {
	CanaryTrafficPercent int           `json:"canaryTrafficPercent" validate:"required,min=1,max=100"`
	Duration             time.Duration `json:"duration" validate:"required,min=1s"`
}

// ABConfig defines configuration for A/B deployments
type ABConfig struct {
	ServiceName  string        `json:"serviceName" validate:"required"`
	RoutingRules []RoutingRule `json:"routingRules" validate:"required,dive"`
	HealthCheck  string        `json:"healthCheck,omitempty"`
}

// RoutingRule defines routing rules for A/B testing
type RoutingRule struct {
	HeaderKey   string `json:"headerKey" validate:"required"`
	HeaderValue string `json:"headerValue" validate:"required"`
	Variant     string `json:"variant" validate:"required,oneof=a b"`
	Weight      int    `json:"weight" validate:"required,min=0,max=100"`
}

// FeatureFlagConfig defines configuration for feature-flag deployments
type FeatureFlagConfig struct {
	ConfigMapName string            `json:"configMapName" validate:"required"`
	Flags         map[string]string `json:"flags" validate:"required"`
	RolloutPercent int               `json:"rolloutPercent" validate:"required,min=0,max=100"`
	HealthCheck   string            `json:"healthCheck,omitempty"`
}

// HealthCheckConfig defines health check parameters
type HealthCheckConfig struct {
	Enabled           bool                    `json:"enabled"`
	PrometheusQueries []PrometheusMetricQuery `json:"prometheusQueries,omitempty"`
	Timeout           time.Duration           `json:"timeout" validate:"min=1s"`
	RetryInterval     time.Duration           `json:"retryInterval" validate:"min=1s"`
	MaxRetries        int                     `json:"maxRetries" validate:"min=0"`
}

// PrometheusMetricQuery defines a Prometheus query for health checks
type PrometheusMetricQuery struct {
	Name        string  `json:"name" validate:"required"`
	Query       string  `json:"query" validate:"required"`
	Threshold   float64 `json:"threshold" validate:"required"`
	Operator    string  `json:"operator" validate:"required,oneof=gt gte lt lte eq ne"`
	Description string  `json:"description,omitempty"`
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
	Offset    int    `json:"offset,omitempty" validate:"min=0"`
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

// DeploymentStrategy defines the interface for deployment strategies
type DeploymentStrategy interface {
	Name() string
	Validate(request *DeploymentRequest) error
	Deploy(ctx context.Context, request *DeploymentRequest) (*DeploymentStatus, error)
	Rollback(ctx context.Context, request *RollbackRequest) error
	// GetStatus(ctx context.Context, namespace, name string) (*DeploymentStatus, error)
}

// // pkg/types/deployment.go
// package types

// import (
// 	"context"
// 	"time"
// )

// // DeploymentRequest represents the request for creating or updating a deployment
type DeploymentRequest struct {
	Namespace         string            `json:"namespace" validate:"required"`
	AppName           string            `json:"name" validate:"required"`
	Image             string            `json:"image" validate:"required"`
	NewImage          string            `json:"new_image"`
	Replicas          int32              `json:"replicas" validate:"required,min=1"`
	Strategy          string            `json:"strategy" validate:"required,oneof=blue-green canary"`
	BlueGreenConfig   *BlueGreenConfig  `json:"blueGreenConfig,omitempty" validate:"required_if=Strategy blue-green"`
	CanaryConfig      *CanaryConfig     `json:"canaryConfig,omitempty" validate:"required_if=Strategy canary"`
	StrategyOverride  *StrategyOverride `json:"strategyOverride,omitempty"` // For updates only
	HealthCheckConfig *HealthCheckConfig `json:"healthCheckConfig,omitempty"`
}

// // StrategyOverride allows overriding the strategy for updates
// type StrategyOverride struct {
// 	Strategy        string           `json:"strategy" validate:"required,oneof=blue-green canary"`
// 	BlueGreenConfig *BlueGreenConfig `json:"blueGreenConfig,omitempty" validate:"required_if=Strategy blue-green"`
// 	CanaryConfig    *CanaryConfig    `json:"canaryConfig,omitempty" validate:"required_if=Strategy canary"`
// }

// // BlueGreenConfig defines configuration for blue-green deployments
// type BlueGreenConfig struct {
// 	ServiceName       string `json:"serviceName" validate:"required"`
// 	ActiveEnvironment string `json:"activeEnvironment" validate:"required,oneof=blue green"`
// 	HealthCheck       string `json:"healthCheck,omitempty"`
// }

// // CanaryConfig defines configuration for canary deployments
// type CanaryConfig struct {
// 	TrafficSteps      []TrafficStep `json:"trafficSteps" validate:"required"`
// 	MaxTrafficPercent int           `json:"maxTrafficPercent" validate:"required,min=1,max=100"`
// 	ServiceName       string        `json:"serviceName" validate:"required"`
// }

// // TrafficStep represents a step in traffic shifting for canary
// type TrafficStep struct {
// 	CanaryTrafficPercent int           `json:"canaryTrafficPercent" validate:"required,min=1,max=100"`
// 	Duration             time.Duration `json:"duration" validate:"required,min=1s"`
// }

// // HealthCheckConfig defines health check parameters
// type HealthCheckConfig struct {
// 	Enabled           bool                    `json:"enabled"`
// 	PrometheusQueries []PrometheusMetricQuery `json:"prometheusQueries,omitempty"`
// 	Timeout           time.Duration           `json:"timeout" validate:"min=1s"`
// 	RetryInterval     time.Duration           `json:"retryInterval" validate:"min=1s"`
// 	MaxRetries        int                     `json:"maxRetries" validate:"min=0"`
// }

// // PrometheusMetricQuery defines a Prometheus query for health checks
// type PrometheusMetricQuery struct {
// 	Name        string  `json:"name" validate:"required"`
// 	Query       string  `json:"query" validate:"required"`
// 	Threshold   float64 `json:"threshold" validate:"required"`
// 	Operator    string  `json:"operator" validate:"required,oneof=gt gte lt lte eq ne"`
// 	Description string  `json:"description,omitempty"`
// }

// // DeploymentStatus represents the current state of a deployment
// type DeploymentStatus struct {
// 	ID           string                 `json:"id"`
// 	Namespace    string                 `json:"namespace"`
// 	Name         string                 `json:"name"`
// 	Status       string                 `json:"status"` // e.g., "pending", "in-progress", "success", "failed", "rolled-back"
// 	CurrentPhase string                 `json:"currentPhase"`
// 	Error        string                 `json:"error,omitempty"`
// 	StartTime    time.Time              `json:"startTime"`
// 	EndTime      *time.Time             `json:"endTime,omitempty"`
// 	Metadata     map[string]interface{} `json:"metadata"` // e.g., current_traffic_percent, active_deployment
// 	Events       []DeploymentEvent      `json:"events"`
// }

// // DeploymentEvent represents an event during deployment
// type DeploymentEvent struct {
// 	Timestamp time.Time `json:"timestamp"`
// 	Phase     string    `json:"phase"`
// 	Message   string    `json:"message"`
// 	Level     string    `json:"level"` // e.g., "info", "error"
// }

// // RollbackRequest represents a request to roll back a deployment
// type RollbackRequest struct {
// 	Namespace string `json:"namespace" validate:"required"`
// 	Name      string `json:"name" validate:"required"`
// 	Reason    string `json:"reason,omitempty"`
// }

// // ListDeploymentsRequest represents a request to list deployments
// type ListDeploymentsRequest struct {
// 	Namespace string `json:"namespace,omitempty"`
// 	Status    string `json:"status,omitempty"`
// 	Strategy  string `json:"strategy,omitempty"`
// 	Limit     int    `json:"limit,omitempty" validate:"min=0"`
// 	Offset    int    `json:"offset,omitempty" validate:"min=0"`
// }

// // ListDeploymentsResponse represents the response for listing deployments
// type ListDeploymentsResponse struct {
// 	Deployments []DeploymentStatus `json:"deployments"`
// 	Total       int                `json:"total"`
// 	Limit       int                `json:"limit"`
// 	Offset      int                `json:"offset"`
// }

// // HealthCheckResult represents the result of a health check
// type HealthCheckResult struct {
// 	Healthy     bool              `json:"healthy"`
// 	Message     string            `json:"message"`
// 	Details     map[string]string `json:"details,omitempty"`
// 	Metrics     []MetricResult    `json:"metrics,omitempty"`
// 	CheckedAt   time.Time         `json:"checkedAt"`
// }

// // MetricResult represents a metric check result
// type MetricResult struct {
// 	Name        string  `json:"name"`
// 	Value       float64 `json:"value"`
// 	Threshold   float64 `json:"threshold"`
// 	Operator    string  `json:"operator"`
// 	Passed      bool    `json:"passed"`
// 	Description string  `json:"description,omitempty"`
// }

// // DeploymentStrategy defines the interface for deployment strategies
// type DeploymentStrategy interface {
// 	Name() string
// 	Validate(request *DeploymentRequest) error
// 	Deploy(ctx context.Context, request *DeploymentRequest) (*DeploymentStatus, error)
// 	Rollback(ctx context.Context, request *RollbackRequest) error
// 	GetStatus(ctx context.Context, namespace, name string) (*DeploymentStatus, error)
// }

// // type DeploymentRequest struct {
// //     AppName           string            `json:"app_name" yaml:"app_name" validate:"required"`
// //     Namespace         string            `json:"namespace" yaml:"namespace" validate:"required"`
// //     NewImage          string            `json:"new_image" yaml:"new_image" validate:"required"`
// //     Strategy          string            `json:"strategy" yaml:"strategy" validate:"oneof=blue-green canary"`
// //     BlueDeployment    string            `json:"blue_deployment" yaml:"blue_deployment"`
// //     GreenDeployment   string            `json:"green_deployment" yaml:"green_deployment"`
// //     ServiceName       string            `json:"service_name" yaml:"service_name"`
// //     IngressName       string            `json:"ingress_name" yaml:"ingress_name"`
// //     HealthCheck       HealthCheckConfig `json:"health_check" yaml:"health_check"`
// //     Timeout           time.Duration     `json:"timeout" yaml:"timeout"`
// //     Labels            map[string]string `json:"labels" yaml:"labels"`
// // }
// type CanaryDeploymentCreate struct {
//     AppName string `json:"name"`
//     Namespace string `json:"namespace"`
//     ServiceName string `json:"service_name"` // We will create it if it doesn't exist
//     Image string `json:"image"`
//     StableDeployment string `json:"stable_deployment"` // store in db metedata, edit the deloyment parameters and connect to service
//     Replicas int32 `json:"replicas"`
//     HealthCheck HealthCheckConfig  `json:"health_check"`
//     CanaryConfig *CanaryConfig `json:"canary_config"`
// }

// type CanaryDeploymentUpdate struct {
//     NewImage string `json:"new_image"`
//     Replicas  string `json:"replicas"`
//     CanaryDeployment string `json:"canary_deployment"`
//     HealthCheck HealthCheckConfig  `json:"health_check"`
//     CanaryConfig *CanaryConfig `json:"canary_config"`
// }


// type DeploymentRequest struct {
//     Namespace        string        `json:"namespace"`
//     AppName         string        `json:"name"`
//     Image           string        `json:"image"`            // Used for new deployments
//     NewImage        string        `json:"new_image"`        // Used for updates
//     Replicas        int32         `json:"replicas"`
//     StableDeployment string        `json:"stable_deployment"`
//     CanaryDeployment string        `json:"canary_deployment"`
//     BlueDeployment string          `json:"blue_deployment"`
//     GreenDeployment string         `json:"green_deployment"`
//     ServiceName     string        `json:"service_name"`
//     CanaryConfig    *CanaryConfig `json:"canary_config"`
//     HealthCheck     HealthCheckConfig        `json:"health_check"`     // Optional health check endpoint
// }

// type BlueGreenDeploymentRequest struct {
//     AppName        string            `json:"app_name" yaml:"app_name" validate:"required"`
//     Namespace      string            `json:"namespace" yaml:"namespace" validate:"required"`
//     NewImage       string            `json:"new_image" yaml:"new_image" validate:"required"`
//     ServiceName    string            `json:"service_name" yaml:"service_name" validate:"required"`
//     IngressName    string            `json:"ingress_name" yaml:"ingress_name"`
//     HealthCheck    HealthCheckConfig `json:"health_check" yaml:"health_check"`
// }

// type CanaryDeploymentRequest struct {
//     AppName        string           `json:"app_name" yaml:"app_name" validate:"required"`
//     Namespace      string           `json:"namespace" yaml:"namespace" validate:"required"`
//     NewImage       string           `json:"new_image" yaml:"new_image" validate:"required"`
//     ServiceName    string           `json:"service_name" yaml:"service_name" validate:"required"`
//     IngressName    string           `json:"ingress_name" yaml:"ingress_name" validate:"required"`
//     CanaryConfig   CanaryConfig     `json:"canary_config" yaml:"canary_config"`
//     HealthCheck    HealthCheckConfig `json:"health_check" yaml:"health_check"`
// }

// type CanaryConfig struct {
//     InitialTrafficPercent int           `json:"initial_traffic_percent" yaml:"initial_traffic_percent"`
//     TrafficIncrement      int           `json:"traffic_increment" yaml:"traffic_increment"`
//     StepDuration          time.Duration `json:"step_duration" yaml:"step_duration"`
//     MaxTrafficPercent     int           `json:"max_traffic_percent" yaml:"max_traffic_percent"`
//     AutoPromote           bool          `json:"auto_promote" yaml:"auto_promote"`
//     AnalysisInterval      time.Duration `json:"analysis_interval" yaml:"analysis_interval"`
// }
// // type CanaryConfig struct {
// //     InitialTrafficPercentage int               `json:"initial_traffic_percentage" yaml:"initial_traffic_percentage" validate:"required"`
// //     StepDuration             time.Duration     `json:"step_duration" yaml:"step_duration" validate:"required"`
// //     MaxTrafficPercentage     int               `json:"max_traffic_percentage" yaml:"max_traffic_percentage" validate:"required"`
// //     AutoPromote              bool              `json:"auto_promote" yaml:"auto_promote" validate:"required"`
// //     AnalysisInterval         time.Duration     `json:"analysis_interval" yaml:"analysis_interval" validate:"required"`

// //     //Advanced parameters
// //     SuccessThreshold  int           `json:"success_threshold,omitempty" yaml:"success_threshold,omitempty"`
// //     FailureThreshold  int           `json:"failure_threshold,omitempty" yaml:"failure_threshold,omitempty"`
// //     MinStepDuration   time.Duration `json:"min_step_duration,omitempty" yaml:"min_step_duration,omitempty"`
// //     MaxStepDuration   time.Duration `json:"max_step_duration,omitempty" yaml:"max_step_duration,omitempty"`
// // }

// type HealthCheckConfig struct {
//     Enabled           bool          `json:"enabled" yaml:"enabled"`
//     PrometheusQueries []PrometheusQuery `json:"prometheus_queries" yaml:"prometheus_queries"`
//     Timeout           time.Duration `json:"timeout" yaml:"timeout"`
//     RetryInterval     time.Duration `json:"retry_interval" yaml:"retry_interval"`
//     MaxRetries        int           `json:"max_retries" yaml:"max_retries"`
// }

// type PrometheusQuery struct {
//     Name      string  `json:"name" yaml:"name"`
//     Query     string  `json:"query" yaml:"query"`
//     Threshold float64 `json:"threshold" yaml:"threshold"`
//     Operator  string  `json:"operator" yaml:"operator"` // "gt", "lt", "eq"
// }

// type DeploymentEvent struct {
//     Timestamp time.Time `json:"timestamp"`
//     Phase     string    `json:"phase"`
//     Message   string    `json:"message"`
//     Level     string    `json:"level"` // "info", "warn", "error"
// }

// // HealthCheck defines health check configuration
// type HealthCheck struct {
//     Enabled       bool          `json:"enabled" yaml:"enabled"`
//     Path          string        `json:"path,omitempty" yaml:"path,omitempty"`
//     Port          int           `json:"port,omitempty" yaml:"port,omitempty"`
//     Scheme        string        `json:"scheme,omitempty" yaml:"scheme,omitempty"` // http or https
//     Headers       []Header      `json:"headers,omitempty" yaml:"headers,omitempty"`
//     Timeout       time.Duration `json:"timeout" yaml:"timeout"`
//     RetryInterval time.Duration `json:"retry_interval" yaml:"retry_interval"`
    
//     // Prometheus-based health checks
//     PrometheusEnabled bool                    `json:"prometheus_enabled,omitempty" yaml:"prometheus_enabled,omitempty"`
//     MetricQueries     []PrometheusMetricQuery `json:"metric_queries,omitempty" yaml:"metric_queries,omitempty"`
// }

// // Header represents an HTTP header
// type Header struct {
//     Name  string `json:"name" yaml:"name"`
//     Value string `json:"value" yaml:"value"`
// }

// // PrometheusMetricQuery defines a Prometheus query for health checking
// type PrometheusMetricQuery struct {
//     Name        string  `json:"name" yaml:"name"`
//     Query       string  `json:"query" yaml:"query"`
//     Threshold   float64 `json:"threshold" yaml:"threshold"`
//     Operator    string  `json:"operator" yaml:"operator"` // "gt", "gte", "lt", "lte", "eq", "ne"
//     Description string  `json:"description,omitempty" yaml:"description,omitempty"`
// }

// // DeploymentStatus represents the status of a deployment
// type DeploymentStatus struct {
//     ID           string                 `json:"id"`
//     AppName      string                 `json:"app_name"`
//     Namespace    string                 `json:"namespace"`
//     Status       string                 `json:"status"` // "pending", "in-progress", "success", "failed", "rolled-back"
//     CurrentPhase string                 `json:"current_phase"`
//     Error        string                 `json:"error,omitempty"`
//     StartTime    time.Time              `json:"start_time"`
//     EndTime      *time.Time             `json:"end_time,omitempty"`
//     Metadata     map[string]interface{} `json:"metadata"`
//     Events       []DeploymentEvent      `json:"events"`
// }

// // HealthCheckResult represents the result of a health check
// type HealthCheckResult struct {
//     Healthy     bool              `json:"healthy"`
//     Message     string            `json:"message"`
//     Details     map[string]string `json:"details,omitempty"`
//     Metrics     []MetricResult    `json:"metrics,omitempty"`
//     CheckedAt   time.Time         `json:"checked_at"`
// }

// // MetricResult represents the result of a Prometheus metric query
// type MetricResult struct {
//     Name        string  `json:"name"`
//     Value       float64 `json:"value"`
//     Threshold   float64 `json:"threshold"`
//     Operator    string  `json:"operator"`
//     Passed      bool    `json:"passed"`
//     Description string  `json:"description,omitempty"`
// }

// // DeploymentStrategy interface that all strategies must implement
// type DeploymentStrategy interface {
//     Name() string
//     Validate(request *DeploymentRequest) error
//     Deploy(ctx context.Context, request *DeploymentRequest) (*DeploymentStatus, error)
//     Rollback(ctx context.Context, deploymentID string) error
//     GetStatus(ctx context.Context, deploymentID string) (*DeploymentStatus, error)
// }

// // RollbackRequest represents a rollback request
// type RollbackRequest struct {
//     DeploymentID string `json:"deployment_id"`
//     Reason       string `json:"reason,omitempty"`
// }

// // ListDeploymentsRequest represents a request to list deployments
// type ListDeploymentsRequest struct {
//     AppName   string `json:"app_name,omitempty"`
//     Namespace string `json:"namespace,omitempty"`
//     Status    string `json:"status,omitempty"`
//     Limit     int    `json:"limit,omitempty"`
//     Offset    int    `json:"offset,omitempty"`
// }

// // ListDeploymentsResponse represents the response for listing deployments
// type ListDeploymentsResponse struct {
//     Deployments []DeploymentStatus `json:"deployments"`
//     Total       int                `json:"total"`
//     Limit       int                `json:"limit"`
//     Offset      int                `json:"offset"`
// }
