// pkg/api/router.go
package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/api/handlers"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/api/middleware"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/strategies"
)

type Server struct {
    router          *gin.Engine
    server          *http.Server
    config          *config.Config
    logger          *zap.Logger
    strategyFactory *strategies.StrategyFactory
    storage         storage.Interface
    metrics         *monitoring.Metrics
}

func NewServer(
    cfg *config.Config,
    logger *zap.Logger,
    strategyFactory *strategies.StrategyFactory,
    storage storage.Interface,
    metrics *monitoring.Metrics,
) *Server {
    if cfg.Logging.Level == "debug" {
        gin.SetMode(gin.DebugMode)
    } else {
        gin.SetMode(gin.ReleaseMode)
    }
    
    router := gin.New()
    
    return &Server{
        router:          router,
        config:          cfg,
        logger:          logger,
        strategyFactory: strategyFactory,
        storage:         storage,
        metrics:         metrics,
    }
}

func (s *Server) SetupRoutes() {
    // Global middleware
    s.router.Use(gin.Recovery())
    s.router.Use(middleware.LoggingMiddleware(s.logger))
    s.router.Use(middleware.RateLimitMiddleware(&s.config.Security.RateLimit))
    
    // Health check (no auth required)
    s.router.GET("/health", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{
            "status":    "healthy",
            "timestamp": time.Now().Format(time.RFC3339),
        })
    })
    
    // Metrics endpoint (no auth required)
    if s.config.Monitoring.Metrics.Enabled {
        s.router.GET(s.config.Monitoring.Metrics.Path, gin.WrapH(promhttp.Handler()))
    }
    
    // API v1 routes with authentication
    v1 := s.router.Group("/api/v1")
    v1.Use(middleware.AuthMiddleware(&s.config.Security.Auth, s.logger))
    
    // Initialize handlers
    deployHandler := handlers.NewDeployHandler(s.strategyFactory, s.metrics, s.logger)
    statusHandler := handlers.NewStatusHandler(s.storage, s.strategyFactory, s.logger)
    rollbackHandler := handlers.NewRollbackHandler(s.storage,s.strategyFactory, s.metrics, s.logger)
    
    // Deployment routes
    deployments := v1.Group("/deployments")
    {
        deployments.POST("", deployHandler.Deploy)
        deployments.GET("", statusHandler.ListDeployments)
        deployments.GET("/:id", statusHandler.GetDeploymentStatus)
        deployments.POST("/:id/rollback", rollbackHandler.Rollback)
    }
    
    // Strategy routes
    strategies := v1.Group("/strategies")
    {
        strategies.GET("", deployHandler.GetSupportedStrategies)
    }
    
    // Status routes
    status := v1.Group("/status")
    {
        status.GET("/health", statusHandler.GetHealth)
    }
}

func (s *Server) Start() error {
    s.SetupRoutes()
    
    s.server = &http.Server{
        Addr:         fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port),
        Handler:      s.router,
        ReadTimeout:  s.config.Server.ReadTimeout,
        WriteTimeout: s.config.Server.WriteTimeout,
        IdleTimeout:  s.config.Server.IdleTimeout,
    }
    
    s.logger.Info("starting HTTP server",
        zap.String("addr", s.server.Addr),
        zap.Bool("tls", s.config.Security.TLS.Enabled))
    
    if s.config.Security.TLS.Enabled {
        return s.server.ListenAndServeTLS(
            s.config.Security.TLS.CertFile,
            s.config.Security.TLS.KeyFile,
        )
    }
    
    return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
    s.logger.Info("shutting down HTTP server")
    return s.server.Shutdown(ctx)
}