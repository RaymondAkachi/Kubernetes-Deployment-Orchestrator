package orchestrator

import (
	"context"
	"fmt"
	"os"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/strategies"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/istio"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
	"go.uber.org/zap"

	istioNetworkingV1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Config holds orchestrator configuration
type Config struct {
	KubeConfig    *rest.Config
	Namespace     string
	Name          string
	Storage       storage.Interface
	// IstioManager  *istio.IstioManager
	// HealthMonitor *monitoring.HealthMonitor
	Logger        *zap.Logger
	// AuditLogger   *logging.AuditLogger
}

// Orchestrator manages deployment operations
type Orchestrator struct {
	kubeClient    *kubernetes.Client
	storage       storage.Interface
	istioMgr      *istio.IstioManager
	healthMonitor *monitoring.HealthMonitor
	Logger        *zap.Logger
	// auditLogger   *logging.AuditLogger
	leaderMgr     *kubernetes.LeaderElectionManager
	strategyMap   map[string]strategies.DeploymentStrategy
}

// NewOrchestrator initializes the orchestrator
func NewOrchestrator(cfg *Config, logger *zap.Logger) (*Orchestrator, error) {

	//CONFIG_PATH WILL CONTAIN WHERE THE USER'S CONFIGMAP IS MOUNTED
	config, err := config.LoadConfig(os.Getenv("CONFIG_PATH"))
	if err != nil {
		return nil, fmt.Errorf("there was an error utilizing the config.yaml file path, %v", err)
	}

	clientset, err := kubernetes.NewClient(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	leaderMgr, err := kubernetes.NewLeaderElectionManager(cfg.KubeConfig, cfg.Namespace, cfg.Name, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize leader election: %w", err)
	}

	prometheusClient, err := monitoring.NewPrometheusClient(&config.Prometheus, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize prometheus client: %w", err)
	}

	metrics := monitoring.NewMetrics()
	health := monitoring.NewHealthMonitor(prometheusClient, clientset, logger, metrics)
	newMongoClient, err := storage.NewMongoDBClient(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to establish connection to mongodb database")
	}

	// istioClient := istio.IstioManager
	// Initialize controller-runtime client
	mgr, err := manager.New(clientset.GetConfig(), manager.Options{
		Scheme: runtime.NewScheme(),
	})
	if err != nil {
		logger.Fatal("failed to create controller-runtime manager", zap.Error(err))
	}

	err = istioNetworkingV1alpha3.AddToScheme(mgr.GetScheme())
	if err != nil {
		logger.Fatal("failed to add Istio scheme to controller-runtime", zap.Error(err))
	}

	crClient := mgr.GetClient()

	// Initialize Istio manager
	istioMgr, err := istio.NewIstioManager(config.Istio, logger, crClient)
	if err != nil {
		logger.Fatal("failed to initialize Istio manager", zap.Error(err))
	}

	orch := &Orchestrator{
		kubeClient:    clientset,
		storage:       cfg.Storage,
		istioMgr:      &istioMgr,
		healthMonitor: health,
		Logger:        logger,
		// auditLogger:   cfg.AuditLogger,
		leaderMgr:     leaderMgr,
		strategyMap:   make(map[string]strategies.DeploymentStrategy),
	}

	// Register strategies
	orch.strategyMap["blue-green"] = strategies.NewBlueGreenStrategy(clientset, health, newMongoClient, logger)
	orch.strategyMap["canary"] = strategies.NewCanaryStrategy(clientset, istioMgr, health, newMongoClient, logger)
	// orch.strategyMap["ab"] = strategies.NewABStrategy(clientset, cfg.IstioManager, cfg.Storage, cfg.Logger)

	return orch, nil
}

// Start begins leader election and background tasks
func (o *Orchestrator) Start(ctx context.Context) error {
	go o.leaderMgr.Start(ctx)
	return nil
}

// Stop shuts down the orchestrator
func (o *Orchestrator) Stop(ctx context.Context) {
	o.leaderMgr.Stop()
	if err := o.storage.Close(ctx); err != nil{
		o.Logger.Info("failed to close database connection", zap.Error(err))
	}
	o.Logger.Info("stopping orchestrator")
}

// IsLeader checks if this instance is the leader
func (o *Orchestrator) IsLeader() bool {
	return o.leaderMgr.IsLeader()
}

// CreateDeployment handles deployment creation
func (o *Orchestrator) CreateDeployment(ctx context.Context, req *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error) {
	if !o.IsLeader() {
		return nil, fmt.Errorf("not the leader instance")
	}

	// Check for existing deployment
	if _, err := o.storage.GetDeploymentByAppNamespace(ctx, req.Namespace, req.Name); err == nil {
		return nil, fmt.Errorf("deployment %s already exists in namespace %s", req.Name, req.Namespace)
	}

	strategy, exists := o.strategyMap[req.Strategy]
	if !exists {
		return nil, fmt.Errorf("unsupported strategy: %s", req.Strategy)
	}

	status, err := strategy.CreateAppDeployment(ctx, req)

	if err != nil {
        o.Logger.Error("create_deployment_failed",
            zap.String("action", "create_deployment"),
            zap.String("subject", req.Name),
            zap.String("result", "failure"),
            zap.String("namespace", req.Namespace),
            zap.Error(err),
        )
        return nil, fmt.Errorf("an error occured while trying to process your request: %v", err)
    }

    o.Logger.Info("create_deployment_success",
        zap.String("action", "create_deployment"),
        zap.String("subject", req.Name),
        zap.String("result", "success"),
        zap.String("namespace", req.Namespace),
        zap.String("status", status.Status),
    )

	return status, err
}

// UpdateDeployment handles deployment updates
func (o *Orchestrator) UpdateDeployment(ctx context.Context, req *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	if !o.IsLeader() {
		return nil, fmt.Errorf("not the leader instance")
	}

	status, err := o.storage.GetDeploymentByAppNamespace(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, fmt.Errorf("deployment not found: %w", err)
	}

	strategy, exists := o.strategyMap[status.Strategy]
	if !exists {
		return nil, fmt.Errorf("unsupported strategy: %s", status.Metadata["strategy"])
	}

	updatedStatus, err := strategy.UpdateAppDeployment(ctx, req)

	if err != nil {
        o.Logger.Error("update_deployment_failed",
            zap.String("action", "update_deployment"),
            zap.String("subject", req.Name),
            zap.String("result", "failure"),
            zap.String("namespace", req.Namespace),
            zap.Error(err),
        )
        return nil, fmt.Errorf("an error occured while trying to process your request: %v", err)
    }

    o.Logger.Info("update_deployment_success",
        zap.String("action", "update_deployment"),
        zap.String("subject", req.Name),
        zap.String("result", "success"),
        zap.String("namespace", req.Namespace),
        zap.String("status", updatedStatus.Status),
    )

	return updatedStatus, err
}

// RollbackDeployment handles deployment rollbacks
func (o *Orchestrator) RollbackDeployment(ctx context.Context, req *types.RollbackRequest) (*storage.DeploymentStatus, error) {
	if !o.IsLeader() {
		return nil, fmt.Errorf("not the leader instance")
	}

	status, err := o.storage.GetDeploymentByAppNamespace(ctx, req.Namespace, req.Name)
	if err != nil {
		return nil, fmt.Errorf("deployment not found: %w", err)
	}

	strategy, exists := o.strategyMap[status.Metadata["strategy"].(string)]
	if !exists {
		return nil, fmt.Errorf("unsupported strategy: %s", status.Metadata["strategy"])
	}

	status, err = strategy.Rollback(ctx, req.Namespace, req.Name)
	if err != nil {
        o.Logger.Error("rollback_deployment_failed",
            zap.String("action", "rollback_deployment"),
            zap.String("subject", req.Name),
            zap.String("result", "failure"),
            zap.String("namespace", req.Namespace),
            zap.Error(err),
        )
        return nil, fmt.Errorf("an error occured while trying to process your request: %v", err)
    }

    o.Logger.Info("rollback_deployment_success",
        zap.String("action", "rollback_deployment"),
        zap.String("subject", req.Name),
        zap.String("result", "success"),
        zap.String("namespace", req.Namespace),
        zap.String("status", status.Status),
    )
	return status, err
}

// GetDeploymentStatus retrieves deployment status
func (o *Orchestrator) GetDeploymentStatus(ctx context.Context, namespace, name string) (*storage.DeploymentStatus, error) {
	// Status can be retrieved by any instance (no leader check)
	status, err := o.storage.GetDeploymentByAppNamespace(ctx, namespace, name)
    if err != nil {
        o.Logger.Info("get_deployment_status_failed",
            zap.String("action", "get_deployment_status"),
            zap.String("subject", name),
            zap.String("result", "failure"),
            zap.String("namespace", namespace),
            zap.Error(err),
        )
        return nil, err
    }

    o.Logger.Info("get_deployment_status_success",
        zap.String("action", "get_deployment_status"),
        zap.String("subject", name),
        zap.String("result", "success"),
        zap.String("namespace", namespace),
        zap.String("status", status.Status),
    )

    return status, nil
}