// pkg/logging/audit.go
package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type AuditEvent struct {
    Timestamp    time.Time   `json:"timestamp"`
    EventType    string      `json:"event_type"`
    DeploymentID string      `json:"deployment_id,omitempty"`
    AppName      string      `json:"app_name,omitempty"`
    Namespace    string      `json:"namespace,omitempty"`
    User         string      `json:"user,omitempty"`
    Action       string      `json:"action"`
    Result       string      `json:"result"`
    Details      interface{} `json:"details,omitempty"`
    Error        string      `json:"error,omitempty"`
}

type AuditLogger struct {
    logger   *zap.Logger
    file     *os.File
    enabled  bool
}



func NewAuditLogger(cfg *config.AuditConfig, logger *zap.Logger) (*AuditLogger, error) {
    auditLogger := &AuditLogger{
        logger:  logger,
        enabled: cfg.Enabled,
    }
    
    if cfg.Enabled && cfg.Path != "" {
        file, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
        if err != nil {
            return nil, fmt.Errorf("failed to open audit log file: %w", err)
        }
        auditLogger.file = file
    }
    
    return auditLogger, nil
}

func (al *AuditLogger) LogDeploymentStart(request *types.DeploymentCreateRequest, deploymentID, user string) {
    if !al.enabled {
        return
    }
    
    event := AuditEvent{
        Timestamp:    time.Now(),
        EventType:    "deployment",
        DeploymentID: deploymentID,
        AppName:      request.Name,
        Namespace:    request.Namespace,
        User:         user,
        Action:       "start",
        Result:       "initiated",
        Details:      request,
    }
    
    al.writeEvent(event)
}

func (al *AuditLogger) LogDeploymentComplete(status *types.DeploymentStatus, user string) {
    if !al.enabled {
        return
    }
    
    event := AuditEvent{
        Timestamp:    time.Now(),
        EventType:    "deployment",
        DeploymentID: status.ID,
        AppName:      status.Name,
        Namespace:    status.Namespace,
        User:         user,
        Action:       "complete",
        Result:       status.Status,
        Details:      status,
    }
    
    if status.Error != "" {
        event.Error = status.Error
    }
    
    al.writeEvent(event)
}

func (al *AuditLogger) LogRollback(deploymentID, user string, success bool, err error) {
    if !al.enabled {
        return
    }
    
    result := "success"
    errorMsg := ""
    if !success {
        result = "failed"
        if err != nil {
            errorMsg = err.Error()
        }
    }
    
    event := AuditEvent{
        Timestamp:    time.Now(),
        EventType:    "rollback",
        DeploymentID: deploymentID,
        User:         user,
        Action:       "rollback",
        Result:       result,
        Error:        errorMsg,
    }
    
    al.writeEvent(event)
}

func (al *AuditLogger) writeEvent(event AuditEvent) {
    // Log to structured logger
    al.logger.Info("audit event",
        zap.String("event_type", event.EventType),
        zap.String("deployment_id", event.DeploymentID),
        zap.String("action", event.Action),
        zap.String("result", event.Result),
        zap.String("user", event.User))
    
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

func (al *AuditLogger) Close() error {
    if al.file != nil {
        return al.file.Close()
    }
    return nil
}