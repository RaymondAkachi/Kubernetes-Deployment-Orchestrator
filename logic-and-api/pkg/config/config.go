// pkg/config/config.go
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

// Config represents the orchestrator's configuration
type Config struct {
	Kubernetes   KubernetesConfig   `mapstructure:"kubernetes" validate:"required"`
	Security     SecurityConfig     `mapstructure:"security" validate:"required"`
	Logging      LoggingConfig      `mapstructure:"logging" validate:"required"`
	Orchestrator OrchestratorConfig `mapstructure:"orchestrator" validate:"required"`
	Storage      StorageConfig      `mapstructure:"storage" validate:"required"`
	Istio        IstioConfig        `mapstructure:"istio" validate:"required"`
	Prometheus  PrometheusConfig   `mapstructure:"prometheus" validate:"required"`
}

// KubernetesConfig defines Kubernetes client settings
type KubernetesConfig struct {
	InCluster  bool          `mapstructure:"in_cluster" validate:"boolean"`
	// ConfigPath string        `mapstructure:"config_path"`
	APIServer  string        `mapstructure:"api_server" validate:"required_if=InCluster false,uri"`
	CACert     string        `mapstructure:"ca_cert" validate:"required_if=InCluster false"`
	Timeout    time.Duration `mapstructure:"timeout" validate:"required,min=1s"`
	RateLimit  RateLimit     `mapstructure:"rate_limit" validate:"required"`
	RetryCount int           `mapstructure:"retry_count" validate:"required,min=1"`
}

// RateLimit defines Kubernetes API rate limiting
type RateLimit struct {
	QPS   float32 `mapstructure:"qps" validate:"required,min=1"`
	Burst int     `mapstructure:"burst" validate:"required,min=1"`
}

// SecurityConfig defines security settings
type SecurityConfig struct {
	Auth      AuthConfig      `mapstructure:"auth" validate:"required"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit" validate:"required"`
	TLS       TLSConfig       `mapstructure:"tls" validate:"required"`
}

type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled" validate:"boolean"`
	CertFile string `mapstructure:"cert_file" validate:"required_if=Enabled true"`
	KeyFile  string `mapstructure:"key_file" validate:"required_if=Enabled true"`
}

// AuthConfig specifies authentication settings
type AuthConfig struct {
	Type string `mapstructure:"type" validate:"required,eq=k8s_token"`
	Enabled bool   `mapstructure:"enabled" validate:"boolean"`
}

// RateLimitConfig defines HTTP request rate limiting
type RateLimitConfig struct {
	Enabled          bool `mapstructure:"enabled" validate:"boolean"`
	RequestsPerSecond int  `mapstructure:"requests_per_second" validate:"required_if=Enabled true,min=1"`
	Burst            int  `mapstructure:"burst" validate:"required_if=Enabled true,min=1"`
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
	DefaultNamespace         string        `mapstructure:"default_namespace" validate:"required"`
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
type IstioConfig struct {
	Enabled   bool   `mapstructure:"enabled" validate:"boolean"`
	APIServer string `mapstructure:"api_server" validate:"required_if=Enabled true,uri"`
	CACert    string `mapstructure:"ca_cert" validate:"required_if=Enabled true"`
	InCluster bool   `mapstructure:"in_cluster" validate:"boolean"`
	ConfigPath string `mapstructure:"config_path" validate:"required_if=InCluster false"`
}

type PrometheusConfig struct {
	URL string `mapstructure:"url" validate:"required,uri"`
}

// Load reads and validates the configuration
func Load() (*Config, error) {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/etc/orchestrator/config.yaml"
	}

	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	setDefaults()
	viper.AutomaticEnv()
	viper.SetEnvPrefix("ORCHESTRATOR")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// setDefaults configures default values
func setDefaults() {
	viper.SetDefault("kubernetes.in_cluster", true)
	viper.SetDefault("kubernetes.config_path", "")
	viper.SetDefault("kubernetes.api_server", "https://kubernetes.default.svc")
	viper.SetDefault("kubernetes.ca_cert", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	viper.SetDefault("kubernetes.timeout", "10s")
	viper.SetDefault("kubernetes.rate_limit.qps", 10)
	viper.SetDefault("kubernetes.rate_limit.burst", 20)
	viper.SetDefault("kubernetes.retry_count", 3)

	viper.SetDefault("security.auth.type", "k8s_token")
	viper.SetDefault("security.rate_limit.enabled", true)
	viper.SetDefault("security.rate_limit.requests_per_second", 10)
	viper.SetDefault("security.rate_limit.burst", 20)

	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.audit.enabled", true)
	viper.SetDefault("logging.audit.path", "/var/log/orchestrator/audit.log")
	viper.SetDefault("logging.path", "/var/log/orchestrator/orchestrator.log")

	viper.SetDefault("orchestrator.max_concurrent_deployments", 5)
	viper.SetDefault("orchestrator.deployment_timeout", "5m")
	viper.SetDefault("orchestrator.default_namespace", "default")

	viper.SetDefault("storage.mongodb.uri", "mongodb://mongodb-service.default.svc.cluster.local:27017")
	viper.SetDefault("storage.mongodb.database", "orchestrator")
	viper.SetDefault("storage.mongodb.collection", "deployments")

	viper.SetDefault("istio.enabled", true)
	viper.SetDefault("istio.api_server", "https://istiod.istio-system.svc:15012")
	viper.SetDefault("istio.ca_cert", "/var/run/secrets/istio/ca.crt")
}

// validateConfig validates the configuration using validator
// func validateConfig(config *Config) error {
// 	validate := validator.New()
// 	return validate.Struct(config)
// }