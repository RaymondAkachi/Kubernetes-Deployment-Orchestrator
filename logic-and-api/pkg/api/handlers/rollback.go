// pkg/api/handlers/rollback.go
package handlers

import (
	"net/http"
	"time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/strategies"
)

type RollbackHandler struct {
    storage          storage.Interface
    strategyFactory *strategies.StrategyFactory
    metrics         *monitoring.Metrics
    logger          *zap.Logger
}

func NewRollbackHandler(storage storage.Interface, strategyFactory *strategies.StrategyFactory, metrics *monitoring.Metrics, logger *zap.Logger) *RollbackHandler {
    return &RollbackHandler{
        storage:         storage,
        strategyFactory: strategyFactory,
        metrics:         metrics,
        logger:          logger,
    }
}

func (h *RollbackHandler) Rollback(c *gin.Context) {
    deploymentID := c.Param("id")
    if deploymentID == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Deployment ID is required",
        })
        return
    }
    
    // Get deployment status to determine strategy
    status, err := h.storage.GetDeploymentStatus(deploymentID)
    if err != nil {
        if err == storage.ErrNotFound {
            c.JSON(http.StatusNotFound, gin.H{
                "error": "Deployment not found",
            })
            return
        }
        
        h.logger.Error("failed to get deployment status for rollback", zap.String("id", deploymentID), zap.Error(err))
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Failed to retrieve deployment status",
        })
        return
    }
    
    strategyName, ok := status.Metadata["strategy"].(string)
    if !ok {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Cannot determine deployment strategy",
        })
        return
    }
    
    strategy, err := h.strategyFactory.Get(strategyName)
    if err != nil {
        h.logger.Error("unknown deployment strategy for rollback", zap.String("strategy", strategyName), zap.Error(err))
        c.JSON(http.StatusBadRequest, gin.H{
            "error":   "Unknown deployment strategy",
            "details": err.Error(),
        })
        return
    }
    
    // Execute rollback
    ctx := c.Request.Context()
    if err := strategy.Rollback(ctx, deploymentID); err != nil {
        h.metrics.RecordDeploymentError()
        h.logger.Error("rollback failed", zap.String("id", deploymentID), zap.Error(err))
        c.JSON(http.StatusInternalServerError, gin.H{
            "error":   "Rollback failed",
            "details": err.Error(),
        })
        return
    }
    
    h.metrics.RecordRollback()
    h.logger.Info("rollback completed", zap.String("id", deploymentID))
    
    c.JSON(http.StatusOK, gin.H{
        "message":       "Rollback completed successfully",
        "deployment_id": deploymentID,
        "timestamp":     time.Now().Format(time.RFC3339),
    })
}