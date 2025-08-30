package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// logEvent is a helper to standardize structured logging across handlers.
func (h *Handler) logEvent(logger *zap.Logger, action, subject, result string, err error, fields ...zap.Field) {
	logMessage := fmt.Sprintf("%s_%s", action, result)
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	fields = append(fields,
		zap.String("action", action),
		zap.String("subject", subject),
		zap.String("result", result),
	)
	logger.Info(logMessage, fields...)
}

func (h *Handler) validateStruct(s interface{}) error {
	if err := h.validator.Struct(s); err != nil {
		validationErrors := err.(validator.ValidationErrors)
		var errorMessages []string
		for _, e := range validationErrors {
			switch e.Tag() {
			case "required":
				errorMessages = append(errorMessages, fmt.Sprintf("field %s is required", e.Field()))
			case "min":
				errorMessages = append(errorMessages, fmt.Sprintf("field %s must be at least %s", e.Field(), e.Param()))
			case "oneof":
				errorMessages = append(errorMessages, fmt.Sprintf("field %s must be one of %s", e.Field(), e.Param()))
			case "required_if":
				errorMessages = append(errorMessages, fmt.Sprintf("field %s is required when %s", e.Field(), e.Param()))
			default:
				errorMessages = append(errorMessages, fmt.Sprintf("field %s failed %s validation", e.Field(), e.Tag()))
			}
		}
		return fmt.Errorf("validation failed: %v", errorMessages)
	}
	return nil
}

func (h *Handler) withAppLock(appKey string, fn func()) {
	lock, _ := h.appLocks.LoadOrStore(appKey, &sync.Mutex{})
	mutex := lock.(*sync.Mutex)
	mutex.Lock()
	defer mutex.Unlock()
	fn()
}

// ------------------------------------------------------------------------------------------------------------------------------------------

func (h *Handler) CreateDeployment(c *gin.Context) {
	var req types.DeploymentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "create_deployment", "failure", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate schema
	if err := h.validateStruct(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "create_deployment", "failure", err,
			zap.String("namespace", req.Namespace),
			zap.String("name", req.Name),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	appKey := fmt.Sprintf("%s:%s", req.Namespace, req.Name)
	h.withAppLock(appKey, func() {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
		defer cancel()

		if !h.orchestrator.IsLeader() {
			h.logger.Warn("not leader, cannot process deployment")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Not the leader instance"})
			return
		}

		status, err := h.orchestrator.CreateDeployment(ctx, &req)
		if err != nil {
			h.logEvent(h.logger, "create_deployment", req.Name, "failure", err,
				zap.String("namespace", req.Namespace),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		h.logEvent(h.logger, "create_deployment", req.Name, "success", nil,
			zap.String("namespace", req.Namespace),
			zap.String("status", status.Status),
		)
		c.JSON(http.StatusCreated, status)
	})
}

// ------------------------------------------------------------------------------------------------------------------------------------------

func (h *Handler) UpdateDeployment(c *gin.Context) {
	var req types.DeploymentUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "update_deployment", "failure", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate schema
	if err := h.validateStruct(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "update_deployment", "failure", err,
			zap.String("namespace", req.Namespace),
			zap.String("name", req.Name),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	appKey := fmt.Sprintf("%s:%s", req.Namespace, req.Name)
	h.withAppLock(appKey, func() {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
		defer cancel()

		if !h.orchestrator.IsLeader() {
			h.logger.Warn("not leader, cannot process update")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Not the leader instance"})
			return
		}

		status, err := h.orchestrator.UpdateDeployment(ctx, &req)
		if err != nil {
			h.logEvent(h.logger, "update_deployment", req.Name, "failure", err,
				zap.String("namespace", req.Namespace),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		h.logEvent(h.logger, "update_deployment", req.Name, "success", nil,
			zap.String("namespace", req.Namespace),
			zap.String("status", status.Status),
		)
		c.JSON(http.StatusOK, status)
	})
}

// ------------------------------------------------------------------------------------------------------------------------------------------

func (h *Handler) Rollback(c *gin.Context) {
	var req types.RollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "rollback_deployment", "failure", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate schema
	if err := h.validateStruct(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "rollback_deployment", "failure", err,
			zap.String("namespace", req.Namespace),
			zap.String("name", req.Name),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	appKey := fmt.Sprintf("%s:%s", req.Namespace, req.Name)
	h.withAppLock(appKey, func() {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
		defer cancel()

		if !h.orchestrator.IsLeader() {
			h.logger.Warn("not leader, cannot process rollback")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Not the leader instance"})
			return
		}

		status, err := h.orchestrator.RollbackDeployment(ctx, &req)
		if err != nil {
			h.logEvent(h.logger, "rollback_deployment", req.Name, "failure", err,
				zap.String("namespace", req.Namespace),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		h.logEvent(h.logger, "rollback_deployment", req.Name, "success", nil,
			zap.String("namespace", req.Namespace),
			zap.String("status", status.Status),
		)
		c.JSON(http.StatusOK, status)
	})
}

// ------------------------------------------------------------------------------------------------------------------------------------------

// handleReadiness checks if the pod is the leader for the readiness probe.
func (h *Handler) handleReadiness(c *gin.Context) {
	if h.orchestrator.IsLeader() {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	} else {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
	}
}

// ------------------------------------------------------------------------------------------------------------------------------------------

func(h  *Handler) GetDeploymentStatus( c *gin.Context) {
	var req types.GetDeploymentStatus
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "create_deployment", "failure", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate schema
	if err := h.validateStruct(&req); err != nil {
		h.logEvent(h.logger, "api_validation", "create_deployment", "failure", err,
			zap.String("namespace", req.Namespace),
			zap.String("name", req.Name),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	appKey := fmt.Sprintf("%s:%s", req.Namespace, req.Name)
	h.withAppLock(appKey, func() {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
		defer cancel()

		if !h.orchestrator.IsLeader() {
			h.logger.Warn("not leader, cannot process rollback")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Not the leader instance"})
			return
		}

		status, err := h.orchestrator.GetDeploymentStatus(ctx, req.Namespace, req.Name)
		if err != nil {
			h.logEvent(h.logger, "get_deployment_stat", req.Name, "failure", err,
				zap.String("namespace", req.Namespace),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		h.logEvent(h.logger, "get_deployment_stat", req.Name, "success", nil,
			zap.String("namespace", req.Namespace),
			zap.String("status", status.Status),
		)
		c.JSON(http.StatusOK, status)
	})

}