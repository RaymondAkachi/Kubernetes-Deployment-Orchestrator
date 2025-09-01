package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/api/handlers"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/orchestrator"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/security"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/logging"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	appConfig, err := config.LoadConfig(os.Getenv("CONFIG_PATH"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := logging.NewLogger(&appConfig.Logging)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// 3. Initialize core services
	kClient, err := kubernetes.NewClient(appConfig, logger)
	if err != nil {
		logger.Fatal("failed to create kubernetes client", zap.Error(err))
		os.Exit(1)
	}

	mongoClient, err := storage.NewMongoDBClient(appConfig, logger)
	if err != nil {
		logger.Fatal("failed to initialize mongo client", zap.Error(err))
		os.Exit(1)
	}

	orchestratorConfig := &orchestrator.Config{
		KubeConfig: kClient.GetConfig(),
		Namespace:  "default",
		Name:       "orchestrator",
		Storage:    mongoClient,
		Logger:     logger,
	}

	monitoring.InitMetrics()

	orch, err := orchestrator.NewOrchestrator(orchestratorConfig, logger)
	if err != nil {
		logger.Fatal("failed to initialize orchestrator", zap.Error(err))
		os.Exit(1)
	}

	auditLogger, err := logging.NewAuthLogger(&appConfig.Logging, logger)
	if err != nil {
		logger.Fatal("failed to initialize audit logger", zap.Error(err))
		os.Exit(1)
	}
	
	defer func() {
		if cerr := auditLogger.Close(); cerr != nil {
			logger.Error("failed to close audit logger", zap.Error(cerr))
		}
	}()

	orchCtx, orchCancel := context.WithCancel(context.Background())
	defer orchCancel()
	orch.Start(orchCtx)

	
	r := gin.Default()
	apiHandler := handlers.NewHandler(orch, logger, auditLogger, kClient.GetClientSet(), appConfig)
	apiHandler.SetupRoutes(r)

	
	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", appConfig.Security.Port),
		Handler: r,
	}

	tlsConfig, err := security.LoadAndVerifyTLSConfig(&appConfig.Security.TLS)
	if err != nil {
		logger.Fatal("failed to load TLS configuration", zap.Error(err))
	}

	go func() {
		if tlsConfig != nil {
			server.TLSConfig = tlsConfig
			logger.Info("starting HTTPS server", zap.String("address", server.Addr))
			if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				logger.Fatal("failed to start HTTPS server", zap.Error(err))
			}
		} else {
			logger.Info("starting HTTP server", zap.String("address", server.Addr))
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Fatal("failed to start HTTP server", zap.Error(err))
			}
		}
	}()


	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutdown signal received")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", zap.Error(err))
	}

	// Stop the orchestrator's background tasks
	orch.Stop(shutdownCtx)

	logger.Info("application gracefully shut down")
}