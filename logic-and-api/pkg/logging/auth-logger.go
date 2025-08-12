// pkg/logging/logging.go
package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"go.uber.org/zap"
)

// AuditEvent represents an authentication or authorization event
type AuthEvent struct {
	Timestamp time.Time   `json:"timestamp"`
	Username  string      `json:"username"`
	Action    string      `json:"action"`
	Result    string      `json:"result"` // e.g., "success", "failure"
	Error     string      `json:"error,omitempty"`
	Details   interface{} `json:"details,omitempty"`
}

// AuditLogger manages structured logging for auth events
type AuthLogger struct {
	logger *zap.Logger
	level  string
	path   string
	file   *os.File
	enabled bool
}

// NewAuditLogger creates a new AuditLogger instance
func NewAuthLogger(cfg *config.LoggingConfig, logger *zap.Logger) (*AuthLogger, error) {
	enabled := cfg.Audit.Enabled
	var file *os.File
	var err error

	if enabled && cfg.Audit.Path != "" {
		file, err = os.OpenFile(cfg.Audit.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open audit log file: %w", err)
		}
	}

	return &AuthLogger{
		logger:  logger,
		level:   cfg.Level,
		path:    cfg.Audit.Path,
		file:    file,
		enabled: enabled,
	}, nil
}

// Close closes the audit log file if open
func (al *AuthLogger) Close() error {
	if al.file != nil {
		return al.file.Close()
	}
	return nil
}

// LogAuthenticationAttempt logs an authentication attempt event
func (al *AuthLogger) LogAuthenticationAttempt(username string, action string, details interface{}) {
	if !al.enabled {
		return
	}

	event := AuthEvent{
		Timestamp: time.Now(),
		Username:  username,
		Action:    action,
		Result:    "attempted",
		Details:   details,
	}

	al.writeEvent(event)
}

// LogAuthenticationSuccess logs a successful authentication event
func (al *AuthLogger) LogAuthenticationSuccess(username string, action string, details interface{}) {
	if !al.enabled {
		return
	}

	event := AuthEvent{
		Timestamp: time.Now(),
		Username:  username,
		Action:    action,
		Result:    "success",
		Details:   details,
	}

	al.writeEvent(event)
}

// LogAuthenticationFailure logs a failed authentication event
func (al *AuthLogger) LogAuthenticationFailure(username string, action string, errMsg string, details interface{}) {
	if !al.enabled {
		return
	}

	event := AuthEvent{
		Timestamp: time.Now(),
		Username:  username,
		Action:    action,
		Result:    "failure",
		Error:     errMsg,
		Details:   details,
	}

	al.writeEvent(event)
}

// LogAuthorizationCheck logs an authorization check event
func (al *AuthLogger) LogAuthorizationCheck(username string, action string, result string, details interface{}) {
	if !al.enabled {
		return
	}

	event := AuthEvent{
		Timestamp: time.Now(),
		Username:  username,
		Action:    action,
		Result:    result, // e.g., "allowed", "denied"
		Details:   details,
	}

	al.writeEvent(event)
}

// writeEvent writes the event to the logger and file
func (al *AuthLogger) writeEvent(event AuthEvent) {
	// Log to structured logger
	al.logger.Info("audit event",
		zap.String("action", event.Action),
		zap.String("result", event.Result),
		zap.String("username", event.Username),
		zap.Any("details", event.Details),
		zap.String("error", event.Error))

	// Write to audit file if configured
	if al.file != nil {
		eventJSON, err := json.Marshal(event)
		if err != nil {
			al.logger.Error("failed to marshal audit event", zap.Error(err))
			return
		}

		if _, err := al.file.Write(append(eventJSON, '\n')); err != nil {
			al.logger.Error("failed to write audit event", zap.Error(err))
		}
	}
}
