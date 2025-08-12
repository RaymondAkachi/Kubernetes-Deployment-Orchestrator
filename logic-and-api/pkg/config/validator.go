package config

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)
func validateConfig(config *Config) error {
	validate := validator.New()
	if err := validate.Struct(config); err != nil {
		var errMsg string
		for i, verr := range err.(validator.ValidationErrors) {
			if i > 0 {
				errMsg += "\n"
			}
			errMsg += fmt.Sprintf("Field '%s' failed validation: %s", verr.Namespace(), verr.Tag())
		}
		return fmt.Errorf("configuration validation failed:\n%s", errMsg)
	}
	return nil
}

// func validateConfig(config *Config) error {
// 	// Initialize the validator
// 	validate := validator.New()

// 	// Validate the struct based on tags
// 	err := validate.Struct(config)
// 	if err != nil {
// 		// Format validation errors for better readability
// 		var errMsg string
// 		for i, verr := range err.(validator.ValidationErrors) {
// 			if i > 0 {
// 				errMsg += "\n"
// 			}
// 			errMsg += fmt.Sprintf("Field '%s' failed validation: %s", verr.Namespace(), verr.Tag())
// 		}
// 		return fmt.Errorf("configuration validation failed:\n%s", errMsg)
// 	}

// 	// Custom validation for TLS settings
// 	if config.Security.TLS.Enabled {
// 		if config.Security.TLS.CertFile == "" || config.Security.TLS.KeyFile == "" {
// 			return fmt.Errorf("security.tls.cert_file and security.tls.key_file must be provided when TLS is enabled")
// 		}
// 	}

// 	// Custom validation for Auth settings
// 	if config.Security.Auth.Enabled {
// 		switch config.Security.Auth.Type {
// 		case "token":
// 			if len(config.Security.Auth.Tokens) == 0 {
// 				return fmt.Errorf("security.auth.tokens must be non-empty when auth type is 'token'")
// 			}
// 		case "jwt":
// 			if config.Security.Auth.Secret == "" {
// 				return fmt.Errorf("security.auth.secret must be provided when auth type is 'jwt'")
// 			}
// 		case "mtls":
// 			// No specific fields required beyond TLS, which is validated above
// 		default:
// 			return fmt.Errorf("security.auth.type '%s' is not supported; must be 'token', 'jwt', or 'mtls'", config.Security.Auth.Type)
// 		}
// 	}

// 	// Custom validation for Kubernetes settings
// 	if !config.Kubernetes.InCluster && config.Kubernetes.ConfigPath == "" {
// 		return fmt.Errorf("kubernetes.config_path must be provided when in_cluster is false")
// 	}

// 	// Ensure positive durations where applicable
// 	if config.Server.ReadTimeout <= 0 {
// 		return fmt.Errorf("server.read_timeout must be a positive duration")
// 	}
// 	if config.Server.WriteTimeout <= 0 {
// 		return fmt.Errorf("server.write_timeout must be a positive duration")
// 	}
// 	if config.Server.IdleTimeout <= 0 {
// 		return fmt.Errorf("server.idle_timeout must be a positive duration")
// 	}
// 	if config.Kubernetes.DefaultTimeout <= 0 {
// 		return fmt.Errorf("kubernetes.default_timeout must be a positive duration")
// 	}
// 	if config.Monitoring.Prometheus.Timeout <= 0 {
// 		return fmt.Errorf("monitoring.prometheus.timeout must be a positive duration")
// 	}

// 	// Ensure non-negative or positive integers where applicable
// 	if config.Kubernetes.RetryCount < 0 {
// 		return fmt.Errorf("kubernetes.retry_count must be non-negative")
// 	}
// 	if config.Kubernetes.Burst <= 0 {
// 		return fmt.Errorf("kubernetes.burst must be positive")
// 	}
// 	if config.Security.RateLimit.RPS <= 0 && config.Security.RateLimit.Enabled {
// 		return fmt.Errorf("security.rate_limit.rps must be positive when rate limiting is enabled")
// 	}
// 	if config.Security.RateLimit.Burst <= 0 && config.Security.RateLimit.Enabled {
// 		return fmt.Errorf("security.rate_limit.burst must be positive when rate limiting is enabled")
// 	}

// 	return nil
// }