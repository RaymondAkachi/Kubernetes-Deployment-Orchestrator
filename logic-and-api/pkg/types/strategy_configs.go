package types

import (
	"time"
)

// BlueGreenConfig defines configuration for blue-green deployments
type BlueGreenConfig struct {
	NewServiceName  string   `json:"Newservice"`
	ActiveEnvironment string `json:"activeEnvironment" validate:"required,oneof=blue green"`
	HealthCheck       string `json:"healthCheck,omitempty"`
}

// CanaryConfig defines configuration for canary deployments
type CanaryConfig struct {
	NewServiceName  string        `json:"Newservice"`
    TrafficIncrement      int32           `json:"traffic_increment" yaml:"traffic_increment"`
    StepDuration          time.Duration `json:"step_duration" yaml:"step_duration"`
	InitialTrafficPercent int32       `json:"initialTrafficPercent" validate:"required,min=1,max=100"`
	// TrafficSteps      []TrafficStep `json:"trafficSteps" validate:"required"`
	MaxTrafficPercent int32         `json:"maxTrafficPercent" validate:"required,min=1,max=100"`
	// ServiceName       string        `json:"serviceName" validate:"required"`
	AutoPromote       bool          `json:"autoPromote" validate:"required"`
	AnalysisInterval  time.Duration `json:"analysis_interval" validate:"required"`
}

// TrafficStep represents a step in traffic shifting for canary
type TrafficStep struct {
	CanaryTrafficPercent int32           `json:"canaryTrafficPercent" validate:"required,min=1,max=100"`
	Duration             time.Duration `json:"duration" validate:"required,min=1s"`
}

// ABConfig defines configuration for A/B deployments
type ABConfig struct {
	NewServiceName  string        `json:"Newservice"`
	RoutingRules []RoutingRule `json:"routingRules" validate:"required,dive"`
	HealthCheck  string        `json:"healthCheck,omitempty"`

	//For update request
	TargetVariant string `json:"targetVariant,omitempty" validate:"omitempty,oneof=a b"`
}

// RoutingRule defines routing rules for A/B testing
type RoutingRule struct {
	HeaderKey   string `json:"headerKey" validate:"required"`
	HeaderValue string `json:"headerValue" validate:"required"`
	Variant     string `json:"variant" validate:"required,oneof=a b"`
	Weight      int32    `json:"weight" validate:"required,min=0,max=100"`
}

// FeatureFlagConfig defines configuration for feature-flag deployments
type FeatureFlagConfig struct {
	NewServiceName  string        `json:"Newservice"`
	ConfigMapName string            `json:"configMapName" validate:"required"`
	Flags         map[string]string `json:"flags" validate:"required"`
	RolloutPercent int32               `json:"rolloutPercent" validate:"required,min=0,max=100"`
	HealthCheck   string            `json:"healthCheck,omitempty"`
	MountPath   string            `json:"mountPath,omitempty"` // Path where the config map will be mounted
}