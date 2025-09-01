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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

type Handler struct {
	orchestrator *orchestrator.Orchestrator
	logger       *zap.Logger
	auditLogger  *logging.AuthLogger
	validator    *validator.Validate
	clientset    *kubernetes.Clientset
	appLocks            sync.Map // Universal lock for appComb (namespace:name)
	createSemaphore     chan struct{} // Semaphore for limiting CreateDeployment concurrency
	cfg          *config.Config
}

func NewHandler(orch *orchestrator.Orchestrator, logger *zap.Logger, auditLogger *logging.AuthLogger, clientset *kubernetes.Clientset, cfg *config.Config) *Handler {
	createSemaphore := make(chan struct{}, cfg.Orchestrator.MaxConcurrentDeployments)
	return &Handler{
		orchestrator: orch,
		logger:       logger,
		auditLogger:  auditLogger,
		validator:    validator.New(),
		clientset:    clientset,
		appLocks:           sync.Map{},
		createSemaphore:     createSemaphore,
		cfg:          cfg,
	}
}

func (h *Handler) SetupRoutes(r *gin.Engine) {

    r.GET("/readyz", h.handleReadiness) // Removed authentication for easy kube rprobe
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
    
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
    statusPermissions := []middleware.ResourcePermission{
        {APIGroup: "apps", Resource: "deployments", Verb: "get", Namespace: "*"},
    }
    
    r.Use(middleware.Auth(h.clientset, h.cfg, h.logger, h.auditLogger))
    r.Use(middleware.NonBlockingRateLimiter(float64(h.cfg.Security.RateLimit.RequestsPerSecond), h.cfg.Kubernetes.RateLimit.Burst))
    
    // 4. Define routes with specific RBAC permissions
    r.POST("/deployments", middleware.RBAC(h.clientset, h.logger, h.auditLogger, createPermissions), h.CreateDeployment)
    r.PUT("/deployments/:name", middleware.RBAC(h.clientset, h.logger, h.auditLogger, updatePermissions), h.UpdateDeployment)
    r.POST("/deployments/:name/rollback", middleware.RBAC(h.clientset, h.logger, h.auditLogger, rollbackPermissions), h.Rollback)
    r.GET("/deployments/:name", middleware.RBAC(h.clientset, h.logger, h.auditLogger, statusPermissions), h.GetDeploymentStatus)
	r.GET("/deployments", middleware.RBAC(h.clientset, h.logger, h.auditLogger, statusPermissions), h.ListDeployments)
}	
