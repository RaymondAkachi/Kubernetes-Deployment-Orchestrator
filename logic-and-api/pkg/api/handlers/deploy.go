// pkg/api/handlers/deploy.go
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/strategies"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type DeployHandler struct {
    strategyFactory *strategies.StrategyFactory
    metrics         *monitoring.Metrics
    logger          *zap.Logger
}

func NewDeployHandler(strategyFactory *strategies.StrategyFactory, metrics *monitoring.Metrics, logger *zap.Logger) *DeployHandler {
    return &DeployHandler{
        strategyFactory: strategyFactory,
        metrics:         metrics,
        logger:          logger,
    }
}

func (h *DeployHandler) Deploy(c *gin.Context) {
    var request types.DeploymentRequest
    if err := c.ShouldBindJSON(&request); err != nil {
        h.logger.Error("invalid deployment request", zap.Error(err))
        c.JSON(http.StatusBadRequest, gin.H{
            "error":   "Invalid request format",
            "details": err.Error(),
        })
        return
    }
    
    // Set defaults
    if request.Strategy == "" {
        request.Strategy = "blue-green"
    }
    if request.Timeout == 0 {
        request.Timeout = 5 * time.Minute
    }
    if request.HealthCheck.Timeout == 0 {
        request.HealthCheck.Timeout = 2 * time.Minute
    }
    if request.HealthCheck.RetryInterval == 0 {
        request.HealthCheck.RetryInterval = 10 * time.Second
    }
    
    // Get deployment strategy
    strategy, err := h.strategyFactory.Get(request.Strategy)
    if err != nil {
        h.logger.Error("unknown deployment strategy", zap.String("strategy", request.Strategy), zap.Error(err))
        c.JSON(http.StatusBadRequest, gin.H{
            "error":   "Unknown deployment strategy",
            "details": err.Error(),
        })
        return
    }
    
    // Validate request
    if err := strategy.Validate(&request); err != nil {
        h.logger.Error("deployment request validation failed", zap.Error(err))
        c.JSON(http.StatusBadRequest, gin.H{
            "error":   "Request validation failed",
            "details": err.Error(),
        })
        return
    }
    
    // Record metrics
    h.metrics.RecordDeployment()
    startTime := time.Now()
    
    // Execute deployment asynchronously
    go func() {
        defer func() {
            duration := time.Since(startTime).Seconds()
            h.metrics.RecordDeploymentDuration(duration)
        }()
        
        ctx := c.Request.Context()
        status, err := strategy.Deploy(ctx, &request)
        if err != nil {
            h.metrics.RecordDeploymentError()
            h.logger.Error("deployment failed",
                zap.String("app", request.AppName),
                zap.String("namespace", request.Namespace),
                zap.Error(err))
            return
        }
        
        h.logger.Info("deployment completed",
            zap.String("app", request.AppName),
            zap.String("namespace", request.Namespace),
            zap.String("status", status.Status),
            zap.String("deployment_id", status.ID))
    }()
    
    // Return immediate response with deployment ID
    c.JSON(http.StatusAccepted, gin.H{
        "message":    "Deployment initiated successfully",
        "app_name":   request.AppName,
        "namespace":  request.Namespace,
        "strategy":   request.Strategy,
        "timestamp":  time.Now().Format(time.RFC3339),
    })
}

func (h *DeployHandler) GetSupportedStrategies(c *gin.Context) {
    strategies := h.strategyFactory.List()
    c.JSON(http.StatusOK, gin.H{
        "strategies": strategies,
    })
}


