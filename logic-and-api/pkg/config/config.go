package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"

	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func init() {
    validate = validator.New()
}

// Config represents the orchestrator's configuration
type Config struct {
    Kubernetes   KubernetesConfig   `mapstructure:"kubernetes" validate:"required"`
    Security     SecurityConfig     `mapstructure:"security" validate:"required"`
    Logging      LoggingConfig      `mapstructure:"logging" validate:"required"`
    Orchestrator OrchestratorConfig `mapstructure:"orchestrator" validate:"required"`
    Storage      StorageConfig      `mapstructure:"storage" validate:"required"`
    // Istio        IstioConfig        `mapstructure:"istio" validate:"required"`
    Prometheus   PrometheusConfig   `mapstructure:"prometheus" validate:"required"`
}

// KubernetesConfig defines Kubernetes client settings
type KubernetesConfig struct {
    // InCluster  bool          `mapstructure:"in_cluster" validate:"required,boolean"`
    Timeout    time.Duration `mapstructure:"timeout" validate:"required,min=1s"`
    RateLimit  RateLimit     `mapstructure:"rate_limit" validate:"required"`
    // RetryCount int           `mapstructure:"retry_count" validate:"required,min=1"`
}

// RateLimit defines Kubernetes API rate limiting
type RateLimit struct {
    QPS   float32 `mapstructure:"qps" validate:"required,min=1"`
    Burst int     `mapstructure:"burst" validate:"required,min=1"`
}

// SecurityConfig defines security settings
type SecurityConfig struct {
    Port      string          `mapstructure:"port" validate:"required"`
    // Auth      AuthConfig      `mapstructure:"auth" validate:"required"`
    RateLimit RateLimitConfig `mapstructure:"rate_limit" validate:"required"`
    TLS       TLSConfig       `mapstructure:"tls" validate:"required"`
}

// TLSConfig defines TLS settings
type TLSConfig struct {
    Enabled  bool   `mapstructure:"enabled" validate:"boolean"`
    CertFile string `mapstructure:"cert_file" validate:"required_if=Enabled true"`
    KeyFile  string `mapstructure:"key_file" validate:"required_if=Enabled true"`
    CAFile   string `mapstructure:"ca_file" validate:"required_if=Enabled true"`
}

// AuthConfig specifies authentication settings
// type AuthConfig struct {
//     // We enforce k8s_token as the only type for in-cluster deployment
//     Type string `mapstructure:"type" validate:"required,oneof=k8s_token"`
// }

// RateLimitConfig defines HTTP request rate limiting
type RateLimitConfig struct {
    Enabled           bool `mapstructure:"enabled" validate:"boolean"`
    RequestsPerSecond int  `mapstructure:"requests_per_second" validate:"required_if=Enabled true,min=1"`
    // Burst             int  `mapstructure:"burst" validate:"required_if=Enabled true,min=1"`
}

// LoggingConfig defines logging settings
type LoggingConfig struct {
	Level string      `mapstructure:"level" validate:"required,oneof=debug info warn error"`
	Audit AuditConfig `mapstructure:"audit" validate:"required"`
	Path  string      `mapstructure:"path" validate:"required"`
}

// AuditConfig specifies audit logging settings
type AuditConfig struct {
	Enabled bool   `mapstructure:"enabled" validate:"boolean"`
	Path    string `mapstructure:"path" validate:"required_if=Enabled true"`
}

// OrchestratorConfig defines orchestrator behavior
type OrchestratorConfig struct {
    MaxConcurrentDeployments int           `mapstructure:"max_concurrent_deployments" validate:"required,min=1"`
    DeploymentTimeout        time.Duration `mapstructure:"deployment_timeout" validate:"required,min=1m"`
    // DefaultNamespace         string        `mapstructure:"default_namespace" validate:"required"`
}

// StorageConfig defines storage settings
type StorageConfig struct {
    MongoDB MongoDBConfig `mapstructure:"mongodb" validate:"required"`
}

// MongoDBConfig specifies MongoDB connection settings
type MongoDBConfig struct {
    URI        string `mapstructure:"uri" validate:"required,uri"`
    Database   string `mapstructure:"database" validate:"required"`
    Collection string `mapstructure:"collection" validate:"required"`
}

// IstioConfig defines Istio client settings
// type IstioConfig struct {
//     Enabled   bool `mapstructure:"enabled" validate:"boolean"`
//     InCluster bool `mapstructure:"in_cluster" validate:"boolean"`
// }

// PrometheusConfig defines Prometheus client settings
type PrometheusConfig struct {
    URL string `mapstructure:"url" validate:"required,uri"`
}

// LoadConfig reads configuration from the specified path
func LoadConfig(path string) (*Config, error) {
    viper.SetConfigFile(path)
    if err := viper.ReadInConfig(); err != nil {
        return nil, fmt.Errorf("failed to read config file: %w", err)
    }

    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("failed to unmarshal config: %w", err)
    }

    if err := validate.Struct(&cfg); err != nil {
        return nil, fmt.Errorf("config validation failed: %w", err)
    }

    return &cfg, nil
}