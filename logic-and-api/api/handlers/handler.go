package handlers

import (
	// "context"
	// "net/http"
	"sync"
	// "time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/api/middleware"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/orchestrator"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/logging"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

type Handler struct {
	orchestrator *orchestrator.Orchestrator
	logger       *zap.Logger
	auditLogger  *logging.AuthLogger
	validator    *validator.Validate
	appLocks     sync.Map
	clientset    *kubernetes.Clientset
	cfg          *config.Config
}

func NewHandler(orch *orchestrator.Orchestrator, logger *zap.Logger, auditLogger *logging.AuthLogger, clientset *kubernetes.Clientset, cfg *config.Config) *Handler {
	return &Handler{
		orchestrator: orch,
		logger:       logger,
		auditLogger:  auditLogger,
		validator:    validator.New(),
		clientset:    clientset,
		cfg:          cfg,
	}
}

func (h *Handler) SetupRoutes(r *gin.Engine) {
	// Define endpoint-specific permissions
	createPermissions := []middleware.ResourcePermission{
		{APIGroup: "apps", Resource: "deployments", Verb: "create", Namespace: "*"},
		{APIGroup: "", Resource: "services", Verb: "create", Namespace: "*"},
		{APIGroup: "networking.istio.io", Resource: "virtualservices", Verb: "create", Namespace: "*"},
	}
	updatePermissions := []middleware.ResourcePermission{
		{APIGroup: "apps", Resource: "deployments", Verb: "update", Namespace: "*"},
		{APIGroup: "", Resource: "services", Verb: "update", Namespace: "*"},
		{APIGroup: "networking.istio.io", Resource: "virtualservices", Verb: "update", Namespace: "*"},
	}
	rollbackPermissions := []middleware.ResourcePermission{
		{APIGroup: "apps", Resource: "deployments", Verb: "update", Namespace: "*"},
		{APIGroup: "networking.istio.io", Resource: "virtualservices", Verb: "update", Namespace: "*"},
	}
	// pauseResumePermissions := []middleware.ResourcePermission{
	// 	{APIGroup: "apps", Resource: "deployments", Verb: "update", Namespace: "*"},
	// }
	statusPermissions := []middleware.ResourcePermission{
		{APIGroup: "apps", Resource: "deployments", Verb: "get", Namespace: "*"},
	}

	// Apply global middleware
	r.Use(middleware.Auth(h.clientset, h.cfg, h.logger, h.auditLogger))
	
	// Define routes with specific RBAC permissions
	r.POST("/deployments", middleware.RBAC(h.clientset, h.logger, h.auditLogger, createPermissions), h.CreateDeployment)
	r.PUT("/deployments/:name", middleware.RBAC(h.clientset, h.logger, h.auditLogger, updatePermissions), h.UpdateDeployment)
	r.POST("/deployments/:name/rollback", middleware.RBAC(h.clientset, h.logger, h.auditLogger, rollbackPermissions), h.Rollback)
	// r.POST("/deployments/:name/pause", middleware.RBAC(h.clientset, h.logger, h.auditLogger, pauseResumePermissions), h.PauseDeployment)
	// r.POST("/deployments/:name/resume", middleware.RBAC(h.clientset, h.logger, h.auditLogger, pauseResumePermissions), h.ResumeDeployment)
	r.GET("/deployments/:name", middleware.RBAC(h.clientset, h.logger, h.auditLogger, statusPermissions), h.GetDeploymentStatus)
	r.GET("/readyz", h.handleReadiness)
}

// ... (Rest of the handler code remains the same, including validateStruct, withAppLock, and individual handlers)
