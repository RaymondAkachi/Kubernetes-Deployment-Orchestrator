package config

import (
	"fmt"
	// "net"
	// "net/url"
	"os"
	"path/filepath"

	// "regexp"
	"strconv"
	"strings"
	"time"

	// "github.com/spf13/viper"
	"github.com/go-playground/validator/v10"
)

func validateConfigFile(path string) error {
    if path == "" {
        return fmt.Errorf("config file path cannot be empty")
    }

    // Check if file exists
    info, err := os.Stat(path)
    if os.IsNotExist(err) {
        return fmt.Errorf("config file does not exist: %s", path)
    }
    if err != nil {
        return fmt.Errorf("cannot access config file: %w", err)
    }

    // Check if it's a regular file
    if !info.Mode().IsRegular() {
        return fmt.Errorf("config path is not a regular file: %s", path)
    }

    // Check file permissions (must be readable)
    file, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("config file is not readable: %w", err)
    }
    file.Close()

    // Check file extension
    ext := filepath.Ext(path)
    if ext != ".yaml" && ext != ".yml" {
        return fmt.Errorf("config file must have .yaml or .yml extension, got: %s", ext)
    }

    return nil
}

// performStrictValidation uses the validator library for struct validation
func performStrictValidation(cfg *Config) error {
    if err := validate.Struct(cfg); err != nil {
        var validationErrors []string
        
        for _, err := range err.(validator.ValidationErrors) {
            validationErrors = append(validationErrors, formatValidationError(err))
        }
        
        return fmt.Errorf("validation errors: %v", validationErrors)
    }
    return nil
}

// performBusinessLogicValidation validates business-specific rules
func performBusinessLogicValidation(cfg *Config) error {
    var errors []string

    // Kubernetes validation
    if cfg.Kubernetes.RateLimit.QPS > float32(cfg.Kubernetes.RateLimit.Burst) {
        errors = append(errors, "kubernetes rate_limit: QPS cannot be greater than Burst")
    }

    // Security validation
    if cfg.Security.RateLimit.Enabled {
        if cfg.Security.RateLimit.RequestsPerSecond > 1000 {
            errors = append(errors, "security rate_limit: requests_per_second should not exceed 1000 for production stability")
        }
    }

    // Orchestrator validation
    if cfg.Orchestrator.MaxConcurrentDeployments > 50 {
        errors = append(errors, "orchestrator: max_concurrent_deployments > 50 may cause resource exhaustion")
    }

    // Cross-component validation
    if cfg.Security.TLS.Enabled {
        // Ensure TLS files are in secure directories
        tlsFiles := []string{cfg.Security.TLS.CertFile, cfg.Security.TLS.KeyFile, cfg.Security.TLS.CAFile}
        for _, file := range tlsFiles {
            if !strings.HasPrefix(file, "/etc/ssl/") && !strings.HasPrefix(file, "/var/lib/secrets/") {
                errors = append(errors, fmt.Sprintf("TLS file should be in secure directory (/etc/ssl/ or /var/lib/secrets/): %s", file))
            }
        }
    }

    if len(errors) > 0 {
        return fmt.Errorf("business logic violations: %v", errors)
    }

    return nil
}

// performSecurityValidation validates security-specific configurations
func performSecurityValidation(cfg *Config) error {
    var errors []string

    // Port security validation
    portNum, _ := strconv.Atoi(cfg.Security.Port)
    if portNum < 1024 && portNum != 80 && portNum != 443 {
        errors = append(errors, "using privileged ports (<1024) other than 80/443 requires elevated permissions")
    }

    // MongoDB security validation
    if strings.Contains(cfg.Storage.MongoDB.URI, "localhost") || strings.Contains(cfg.Storage.MongoDB.URI, "127.0.0.1") {
        errors = append(errors, "MongoDB URI should not use localhost/127.0.0.1 in production")
    }

    if !strings.Contains(cfg.Storage.MongoDB.URI, "ssl=true") && !strings.Contains(cfg.Storage.MongoDB.URI, "tls=true") {
        errors = append(errors, "MongoDB connection should use SSL/TLS encryption")
    }

    // Prometheus security validation
    if strings.HasPrefix(cfg.Prometheus.URL, "http://") && !strings.Contains(cfg.Prometheus.URL, "localhost") {
        errors = append(errors, "Prometheus URL should use HTTPS for remote connections")
    }

    // Timeout security validation
    if cfg.Kubernetes.Timeout > 5*time.Minute {
        errors = append(errors, "kubernetes timeout > 5 minutes may cause hanging operations")
    }

    if cfg.Orchestrator.DeploymentTimeout < 5*time.Minute {
        errors = append(errors, "deployment timeout < 5 minutes may cause premature failures")
    }

    if len(errors) > 0 {
        return fmt.Errorf("security violations: %v", errors)
    }

    return nil
}

// // Custom validation functions
// func validatePort(fl validator.FieldLevel) bool {
//     port := fl.Field().String()
//     if port == "" {
//         return false
//     }
    
//     portNum, err := strconv.Atoi(port)
//     if err != nil {
//         return false
//     }
    
//     return portNum > 0 && portNum <= 65535
// }

// func validateMongoDBURI(fl validator.FieldLevel) bool {
//     uri := fl.Field().String()
//     if uri == "" {
//         return false
//     }

//     // Must start with mongodb:// or mongodb+srv://
//     if !strings.HasPrefix(uri, "mongodb://") && !strings.HasPrefix(uri, "mongodb+srv://") {
//         return false
//     }

//     // Basic URL parsing
//     _, err := url.Parse(uri)
//     return err == nil
// }

// func validatePrometheusURL(fl validator.FieldLevel) bool {
//     urlStr := fl.Field().String()
//     if urlStr == "" {
//         return false
//     }

//     u, err := url.Parse(urlStr)
//     if err != nil {
//         return false
//     }

//     // Must be HTTP or HTTPS
//     if u.Scheme != "http" && u.Scheme != "https" {
//         return false
//     }

//     // Must have a host
//     if u.Host == "" {
//         return false
//     }

//     // Validate host is reachable (basic format check)
//     if u.Hostname() != "" {
//         if net.ParseIP(u.Hostname()) == nil {
//             // Not an IP, check if it's a valid hostname
//             // if !isValidHostname(u.Hostname()) {
//             return false
//             // }
//         }
//     }

//     return true
// }

// func validateFilePath(fl validator.FieldLevel) bool {
//     path := fl.Field().String()
//     if path == "" {
//         return false
//     }

//     // Basic path validation - must be absolute for security
//     if !filepath.IsAbs(path) {
//         return false
//     }

//     // Check for directory traversal attempts
//     if strings.Contains(path, "..") {
//         return false
//     }

//     return true
// }

// func validateLogLevel(fl validator.FieldLevel) bool {
//     level := strings.ToLower(fl.Field().String())
//     validLevels := map[string]bool{
//         "debug": true,
//         "info":  true,
//         "warn":  true,
//         "error": true,
//     }
//     return validLevels[level]
// }

// func validateTimeoutDuration(fl validator.FieldLevel) bool {
//     duration := fl.Field().Interface().(time.Duration)
    
//     // Must be at least 1 second
//     if duration < time.Second {
//         return false
//     }
    
//     // Must not exceed 1 hour (reasonable for k8s operations)
//     if duration > time.Hour {
//         return false
//     }
    
//     return true
// }

// Helper functions
// func isValidHostname(hostname string) bool {
//     if len(hostname) > 253 {
//         return false
//     }
    
//     // Regex for valid hostname
//     validHostname := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
//     return validHostname.MatchString(hostname)
// }

func formatValidationError(err validator.FieldError) string {
    field := strings.ToLower(err.Field())
    
    switch err.Tag() {
    case "required":
        return fmt.Sprintf("field '%s' is required", field)
    case "min":
        return fmt.Sprintf("field '%s' must be at least %s", field, err.Param())
    case "max":
        return fmt.Sprintf("field '%s' must not exceed %s", field, err.Param())
    case "oneof":
        return fmt.Sprintf("field '%s' must be one of: %s", field, err.Param())
    case "uri":
        return fmt.Sprintf("field '%s' must be a valid URI", field)
    case "port":
        return fmt.Sprintf("field '%s' must be a valid port number (1-65535)", field)
    case "mongodb_uri":
        return fmt.Sprintf("field '%s' must be a valid MongoDB connection URI", field)
    case "prometheus_url":
        return fmt.Sprintf("field '%s' must be a valid Prometheus HTTP/HTTPS URL", field)
    case "file_path":
        return fmt.Sprintf("field '%s' must be a valid absolute file path", field)
    case "log_level":
        return fmt.Sprintf("field '%s' must be one of: debug, info, warn, error", field)
    case "timeout_duration":
        return fmt.Sprintf("field '%s' must be between 1s and 1h", field)
    default:
        return fmt.Sprintf("field '%s' failed validation: %s", field, err.Tag())
    }
}