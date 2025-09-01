package main

import (
	"fmt"
	"strings"
	"time"
)

type YamlConfig struct {
	Kubernetes   KubernetesConfig   `mapstructure:"kubernetes" validate:"required"`
	Security     SecurityConfig     `mapstructure:"security" validate:"required"`
	Logging      LoggingConfig      `mapstructure:"logging" validate:"required"`
	Orchestrator OrchestratorConfig `mapstructure:"orchestrator" validate:"required"`
	Storage      StorageConfig      `mapstructure:"storage" validate:"required"`
	// Istio        IstioConfig        `mapstructure:"istio" validate:"required"`
	Prometheus PrometheusConfig `mapstructure:"prometheus" validate:"required"`
}

// KubernetesConfig defines Kubernetes client settings
type KubernetesConfig struct {
	// InCluster  bool          `mapstructure:"in_cluster" validate:"required,boolean"`
	Timeout   time.Duration `mapstructure:"timeout" validate:"required,min=1s"`
	RateLimit RateLimit     `mapstructure:"rate_limit" validate:"required"`
	// RetryCount int           `mapstructure:"retry_count" validate:"required,min=1"`
}

// RateLimit defines Kubernetes API rate limiting
type RateLimit struct {
	QPS   float32 `mapstructure:"qps" validate:"required,min=1"`
	Burst int     `mapstructure:"burst" validate:"required,min=1"`
}

// SecurityConfig defines security settings
type SecurityConfig struct {
	Port string `mapstructure:"port" validate:"required"`
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


func validateConfigStruct(cfg *YamlConfig) error {
	var errors []string

	// Validate Kubernetes Config
	if err := validateKubernetesConfig(&cfg.Kubernetes); err != nil {
		errors = append(errors, fmt.Sprintf("kubernetes config: %v", err))
	}

	// Validate Security Config
	if err := validateSecurityConfig(&cfg.Security); err != nil {
		errors = append(errors, fmt.Sprintf("security config: %v", err))
	}

	// Validate Logging Config
	if err := validateLoggingConfig(&cfg.Logging); err != nil {
		errors = append(errors, fmt.Sprintf("logging config: %v", err))
	}

	// Validate Orchestrator Config
	if err := validateOrchestratorConfig(&cfg.Orchestrator); err != nil {
		errors = append(errors, fmt.Sprintf("orchestrator config: %v", err))
	}

	// Validate Storage Config
	if err := validateStorageConfig(&cfg.Storage); err != nil {
		errors = append(errors, fmt.Sprintf("storage config: %v", err))
	}

	// Validate Prometheus Config
	if err := validatePrometheusConfig(&cfg.Prometheus); err != nil {
		errors = append(errors, fmt.Sprintf("prometheus config: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation failed: %v", errors)
	}

	return nil
}

func validateKubernetesConfig(cfg *KubernetesConfig) error {
	var errors []string

	// Validate timeout - must be at least 1 second
	if cfg.Timeout < time.Second {
		errors = append(errors, "timeout must be at least 1 second")
	}

	// Validate RateLimit
	if err := validateRateLimit(&cfg.RateLimit); err != nil {
		errors = append(errors, fmt.Sprintf("rate_limit: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateRateLimit(cfg *RateLimit) error {
	var errors []string

	// QPS must be at least 1
	if cfg.QPS < 1 {
		errors = append(errors, "qps must be at least 1")
	}

	// Burst must be at least 1
	if cfg.Burst < 1 {
		errors = append(errors, "burst must be at least 1")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateSecurityConfig(cfg *SecurityConfig) error {
	var errors []string

	// Port is required
	if cfg.Port == "" {
		errors = append(errors, "port is required")
	}

	// Validate RateLimitConfig
	if err := validateRateLimitConfig(&cfg.RateLimit); err != nil {
		errors = append(errors, fmt.Sprintf("rate_limit: %v", err))
	}

	// Validate TLS Config
	if err := validateTLSConfig(&cfg.TLS); err != nil {
		errors = append(errors, fmt.Sprintf("tls: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateRateLimitConfig(cfg *RateLimitConfig) error {
	var errors []string

	// If enabled, requests_per_second must be at least 1
	if cfg.Enabled && cfg.RequestsPerSecond < 1 {
		errors = append(errors, "requests_per_second must be at least 1 when rate limiting is enabled")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateTLSConfig(cfg *TLSConfig) error {
	var errors []string

	// If TLS is enabled, cert_file, key_file, and ca_file are required
	if cfg.Enabled {
		if cfg.CertFile == "" {
			errors = append(errors, "cert_file is required when TLS is enabled")
		}
		if cfg.KeyFile == "" {
			errors = append(errors, "key_file is required when TLS is enabled")
		}
		if cfg.CAFile == "" {
			errors = append(errors, "ca_file is required when TLS is enabled")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateLoggingConfig(cfg *LoggingConfig) error {
	var errors []string

	// Level must be one of: debug, info, warn, error
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[cfg.Level] {
		errors = append(errors, "level must be one of: debug, info, warn, error")
	}

	// Path is required
	if cfg.Path == "" {
		errors = append(errors, "path is required")
	}

	// Validate AuditConfig
	if err := validateAuditConfig(&cfg.Audit); err != nil {
		errors = append(errors, fmt.Sprintf("audit: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateAuditConfig(cfg *AuditConfig) error {
	var errors []string

	// If audit is enabled, path is required
	if cfg.Enabled && cfg.Path == "" {
		errors = append(errors, "path is required when audit is enabled")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateOrchestratorConfig(cfg *OrchestratorConfig) error {
	var errors []string

	// MaxConcurrentDeployments must be at least 1
	if cfg.MaxConcurrentDeployments < 1 {
		errors = append(errors, "max_concurrent_deployments must be at least 1")
	}

	// DeploymentTimeout must be at least 1 minute
	if cfg.DeploymentTimeout < time.Minute {
		errors = append(errors, "deployment_timeout must be at least 1 minute")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateStorageConfig(cfg *StorageConfig) error {
	var errors []string

	// Validate MongoDB Config
	if err := validateMongoDBConfig(&cfg.MongoDB); err != nil {
		errors = append(errors, fmt.Sprintf("mongodb: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validateMongoDBConfig(cfg *MongoDBConfig) error {
	var errors []string

	// URI is required and must be a valid URI
	if cfg.URI == "" {
		errors = append(errors, "uri is required")
	} else {
		// Basic URI validation - check if it starts with mongodb:// or mongodb+srv://
		if !strings.HasPrefix(cfg.URI, "mongodb://") && !strings.HasPrefix(cfg.URI, "mongodb+srv://") {
			errors = append(errors, "uri must be a valid MongoDB connection string (mongodb:// or mongodb+srv://)")
		}
	}

	// Database name is required
	if cfg.Database == "" {
		errors = append(errors, "database is required")
	}

	// Collection name is required
	if cfg.Collection == "" {
		errors = append(errors, "collection is required")
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}

func validatePrometheusConfig(cfg *PrometheusConfig) error {
	var errors []string

	// URL is required and must be a valid URI
	if cfg.URL == "" {
		errors = append(errors, "url is required")
	} else {
		// Basic URL validation - check if it starts with http:// or https://
		if !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
			errors = append(errors, "url must be a valid HTTP/HTTPS URL")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%v", errors)
	}
	return nil
}