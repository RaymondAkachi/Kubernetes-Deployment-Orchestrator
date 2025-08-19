package storage

import (
	"time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type DeploymentStatus struct {
	ID             string                 `bson:"id"`
	AppName        string                 `bson:"app_name"`
	Namespace      string                 `bson:"namespace"`
	Image          string                 `bson:"image"`
	ServiceName    string                 `bson:"service_name"`
	Replicas       int32                  `bson:"replicas"`
	Strategy       string                 `bson:"strategy"`
	Status         string                 `bson:"status"`        // e.g., "pending", "in-progress", "success", "failed", "rolled-back"
	CurrentPhase   string                 `bson:"current_phase"` // e.g., "initializing", "waiting_ready", "progressive_rollout"
	StartTime      time.Time              `bson:"start_time"`
	EndTime        *time.Time             `bson:"end_time"`
	Error          string                 `bson:"error"`

	//Strategy configurations
	BlueGreenConfig *BlueGreenConfig	  `bson:"blue_green_config,omitempty"`
	CanaryConfig   *CanaryConfig         `bson:"canary_config,omitempty"`
	FeatureFlagConfig *FeatureFlagConfig `bson:"feature_flag_config,omitempty"`
	ABConfig       *ABConfig             `bson:"ab_config,omitempty"`
	HealthCheck    *types.HealthCheckConfig    `bson:"health_check,omitempty"` // Health
	
	
	// Error message if failed
	Metadata       map[string]interface{} `bson:"metadata"` // Additional data (e.g., canary_config, current_traffic_percent)
	// StrategyConfig map[string]interface{} `bson:"strategy_config"`
	Events         []DeploymentEvent      `bson:"events"`
}

type DeploymentEvent struct {
	Timestamp time.Time `bson:"timestamp"`
	Phase     string    `bson:"phase"`   // e.g., "initializing", "progressive_rollout"
	Message   string    `bson:"message"` // e.g., "Starting canary deployment"
	Level     string    `bson:"level"`   // e.g., "info", "error"
}

type BlueGreenConfig struct {
	ActiveDeploymentName   string `bson:"active_deployment_name,omitempty"`
	TargetDeploymentName string `bson:"target_deployment_name,omitempty"`
	ActiveEnvironment      string `bson:"active_environment,omitempty"`
	TargetEnvironment    string `bson:"target_environment,omitempty"`
}

type CanaryConfig struct {
	StableDeploymentName string `bson:"stable_deployment_name,omitempty"`
	CanaryDeploymentName string `bson:"canary_deployment_name,omitempty"`
	StableWeight         int32  `bson:"stable_weight,omitempty"`
	CanaryWeight         int32  `bson:"canary_weight,omitempty"`
	DeploymentConfig    *types.CanaryConfig  `bson:"deployment_config"`
}

type FeatureFlagConfig struct {
	DeploymentName string            `bson:"deployment_name"`
	ConfigMapName string            `bson:"configMapName" validate:"required"`
	Flags         map[string]string `bson:"flags" validate:"required"`
	RolloutPercent int32               `bson:"rolloutPercent" validate:"required,min=0,max=100"`
	HealthCheck   string            `bson:"healthCheck,omitempty"`
}

type ABConfig struct {
	ServiceName  string        `bson:"serviceName" validate:"required"`
	RoutingRules []RoutingRule `bson:"routingRules" validate:"required,dive"`
	HealthCheck  string        `bson:"healthCheck,omitempty"`
}

// RoutingRule defines routing rules for A/B testing
type RoutingRule struct {
	HeaderKey   string `bson:"headerKey" validate:"required"`
	HeaderValue string `bson:"headerValue" validate:"required"`
	Variant     string `bson:"variant" validate:"required,oneof=a b"`
	Weight      int32    `bson:"weight" validate:"required,min=0,max=100"`
}

