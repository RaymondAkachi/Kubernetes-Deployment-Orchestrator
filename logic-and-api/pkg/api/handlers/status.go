// pkg/api/handlers/status.go
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/strategies"
)

type StatusHandler struct {
    storage         storage.Interface
    strategyFactory *strategies.StrategyFactory
    logger          *zap.Logger
}

func NewStatusHandler(storage storage.Interface, strategyFactory *strategies.StrategyFactory, logger *zap.Logger) *StatusHandler {
    return &StatusHandler{
        storage:         storage,
        strategyFactory: strategyFactory,
        logger:          logger,
    }
}

func (h *StatusHandler) GetDeploymentStatus(c *gin.Context) {
    deploymentID := c.Param("id")
    if deploymentID == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Deployment ID is required",
        })
        return
    }
    
    status, err := h.storage.GetDeploymentStatus(deploymentID)
    if err != nil {
        if err == storage.ErrNotFound {
            c.JSON(http.StatusNotFound, gin.H{
                "error": "Deployment not found",
            })
            return
        }
        
        h.logger.Error("failed to get deployment status", zap.String("id", deploymentID), zap.Error(err))
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Failed to retrieve deployment status",
        })
        return
    }
    
    c.JSON(http.StatusOK, status)
}

func (h *StatusHandler) ListDeployments(c *gin.Context) {
    namespace := c.Query("namespace")
    appName := c.Query("app_name")
    status := c.Query("status")
    
    filters := storage.DeploymentFilters{
        Namespace: namespace,
        AppName:   appName,
        Status:    status,
    }
    
    deployments, err := h.storage.ListDeployments(filters)
    if err != nil {
        h.logger.Error("failed to list deployments", zap.Error(err))
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Failed to retrieve deployments",
        })
        return
    }
    
    c.JSON(http.StatusOK, gin.H{
        "deployments": deployments,
        "count":       len(deployments),
    })
}

func (h *StatusHandler) GetHealth(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{
        "status":    "healthy",
        "timestamp": time.Now().Format(time.RFC3339),
        "version":   "1.0.0", // Should come from build info
    })
}