package types

import (
	"time"
)

// BlueGreenConfig defines configuration for blue-green deployments
type BlueGreenConfig struct {
	ServiceConfig        *ServiceConfig  `json:"service_config" validate:"required"`
	ActiveEnvironment    string          `json:"activeEnvironment" validate:"required,oneof=blue green"`
}

// CanaryConfig defines configuration for canary deployments
type CanaryConfig struct {
	ServiceConfig         *ServiceConfig  `json:"service_config" validate:"required"`
    TrafficIncrement      int32           `json:"traffic_increment" validate:"required"`
    StepDuration          time.Duration   `json:"step_duration" validate:"required"`
	InitialTrafficPercent int32           `json:"initialTrafficPercent" validate:"required,min=1,max=100"`
	MaxTrafficPercent     int32           `json:"maxTrafficPercent" validate:"required,min=1,max=100"`
	AutoPromote           bool            `json:"autoPromote" validate:"required"`
	AnalysisInterval      time.Duration   `json:"analysis_interval" validate:"required,min=1s"`
}

// TrafficStep represents a step in traffic shifting for canary
type TrafficStep struct {
	CanaryTrafficPercent int32           `json:"canaryTrafficPercent" validate:"required,min=1,max=100"`
	Duration             time.Duration `json:"duration" validate:"required,min=1s"`
}

// ABConfig defines configuration for A/B deployments
type ABConfig struct {
	ServiceConfig        *ServiceConfig  `json:"service_config"`
	RoutingRules []RoutingRule `json:"routingRules" validate:"required,dive"`
	// HealthCheck  string        `json:"healthCheck,omitempty"`

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

