package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/api/handlers"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/orchestrator"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/logging"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/gin-gonic/gin"
)


func main() {
	config, err := config.LoadConfig(os.Getenv("CONFIG_PATH"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "there was an error utilizing the config.yaml file path, %v", err)
		os.Exit(1)
	}

	logger, err := logging.NewLogger(&config.Logging)
	defer logger.Sync()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	kClient, err := kubernetes.NewClient(config, logger)
	if err != nil{
		fmt.Fprintf(os.Stderr, "Failed to create kubernetes Client: %v\n", err)
		os.Exit(1)
	}

	mongoClient, err := storage.NewMongoDBClient(config, logger)
	if err != nil{
		fmt.Fprintf(os.Stderr, "Failed to initialize mongoClient: %v\n", err)
		os.Exit(1)
	}

	orchestratorConfig := &orchestrator.Config{
        KubeConfig:  kClient.GetConfig(),
        Namespace:   "default",
        Name:        "orchestrator",
        Storage:     mongoClient,
        Logger:      logger,
    }

	monitoring.InitMetrics() // Initialize metrics for prometheus scrapping

	orchestrator, err := orchestrator.NewOrchestrator(orchestratorConfig, logger)
	if err != nil{
		fmt.Fprintf(os.Stderr, "Failed to initialize orchestrator: %v\n", err)
		os.Exit(1)
	}

	auditLogger, err := logging.NewAuthLogger(&config.Logging, logger)
	if err != nil{
		fmt.Fprintf(os.Stderr, "Failed to initialize auditLogger: %v\n", err)
		os.Exit(1)
	}
	
	// Start orchestrator
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := orchestrator.Start(ctx); err != nil {
		logger.Fatal("failed to start orchestrator", zap.Error(err))
	}


	defer func() {
	if cerr := auditLogger.Close(); cerr != nil {
		logger.Fatal("failed to close audit logger", zap.Error(err))
	}
	}()

	//Set-Up gin API
	r := gin.Default()
	apiHandler := handlers.NewHandler(orchestrator, logger, auditLogger, kClient.GetClientSet(), config)
	apiHandler.SetupRoutes(r)

	// Start HTTP server
	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", config.Security.Port),
		Handler: r,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("failed to start server", zap.Error(err))
		}
	}()
	
	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	logger.Info("shutdown signal received")
	ctxShutdown, cancelShutdown := context.WithTimeout(ctx, 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(ctxShutdown); err != nil {
		logger.Error("server shutdown failed", zap.Error(err))
	}
	orchestrator.Stop(ctx)

}