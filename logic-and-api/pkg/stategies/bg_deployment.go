package strategies

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type BlueGreenStrategy struct {
	kubeClient     *kubernetes.Client
	deploymentMgr  *kubernetes.DeploymentManager
	serviceMgr     *kubernetes.ServiceManager
	healthMonitor  *monitoring.HealthMonitor
	storage        storage.Interface
	logger         *zap.Logger
}

func NewBlueGreenStrategy(
	kubeClient *kubernetes.Client,
	healthMonitor *monitoring.HealthMonitor,
	storage storage.Interface,
	logger *zap.Logger,
) *BlueGreenStrategy {
	return &BlueGreenStrategy{
		kubeClient:    kubeClient,
		deploymentMgr: kubernetes.NewDeploymentManager(kubeClient, logger),
		serviceMgr:    kubernetes.NewServiceManager(kubeClient, logger),
		healthMonitor: healthMonitor,
		storage:       storage,
		logger:        logger,
	}
}

func (bg *BlueGreenStrategy) Name() string {
	return "blue-green"
}

func (bg *BlueGreenStrategy) Validate(request *types.DeploymentRequest) error {
	if request.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if request.AppName == "" {
		return fmt.Errorf("name is required")
	}
	if request.Image == "" && request.NewImage == "" {
		return fmt.Errorf("image or new_image is required")
	}
	if request.Replicas < 1 {
		return fmt.Errorf("replicas must be at least 1")
	}
	if request.Strategy != "blue-green" {
		return fmt.Errorf("strategy must be blue-green")
	}
	if request.BlueGreenConfig == nil {
		return fmt.Errorf("blueGreenConfig is required for blue-green strategy")
	}
	if request.BlueGreenConfig.ServiceName == "" {
		return fmt.Errorf("serviceName is required in blueGreenConfig")
	}
	if request.BlueGreenConfig.ActiveEnvironment != "blue" && request.BlueGreenConfig.ActiveEnvironment != "green" {
		return fmt.Errorf("activeEnvironment must be blue or green")
	}
	return nil
}

// func (bg *BlueGreenStrategy) Deploy(ctx context.Context, request *types.DeploymentRequest) (*types.DeploymentStatus, error) {
// 	// This method can be deprecated or used as a wrapper; direct calls to CreateBlueGreenDeployment or UpdateBlueGreenDeployment are preferred
// 	return nil, fmt.Errorf("use CreateBlueGreenDeployment or UpdateBlueGreenDeployment")
// }

func (bg *BlueGreenStrategy) CreateBlueGreenDeployment(ctx context.Context, request *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error) {
	deploymentID := uuid.New().String()
	
	status := &storage.DeploymentStatus{
		ID:           deploymentID,
		Namespace:    request.Namespace,
		AppName:       request.Name,
		Status:       "pending",
		CurrentPhase: "initializing",
		StartTime:    time.Now(),
		Metadata:     make(map[string]interface{}),
		Events:       []storage.DeploymentEvent{},
	}
	
	if err := bg.storage.SaveDeployment(ctx, status); err != nil {
		return nil, fmt.Errorf("failed to store deployment status: %w", err)
	}
	
	bg.addEvent(status, "info", "initializing", "Starting blue-green deployment creation")

	// Check if deployment name is unique
	existing, err := bg.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil && !errors.IsNotFound(err) {
		bg.updateStatusWithError(ctx, status, "failed", "check_unique", err)
		return status, err
	}
	if existing != nil {
		bg.updateStatusWithError(ctx, status, "failed", "check_unique", fmt.Errorf("deployment name %s already exists", request.Name))
		return status, fmt.Errorf("deployment name %s already exists", request.Name)
	}

	blueDeploymentName := fmt.Sprintf("%s-blue", request.Name)
	greenDeploymentName := fmt.Sprintf("%s-green", request.Name)

	activeDeployment := blueDeploymentName
	targetDeployment := greenDeploymentName
	if request.BlueGreenConfig.ActiveEnvironment == "green" {
		activeDeployment = greenDeploymentName
		targetDeployment = blueDeploymentName
	}

	// Create active and target deployments
	if err := bg.createDeployment(ctx, request.Namespace, activeDeployment, request.Image, request.Replicas, request.BlueGreenConfig.ActiveEnvironment); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "create_active", err)
		return status, err
	}
	if err := bg.createDeployment(ctx, request.Namespace, targetDeployment, request.Image, request.Replicas, request.BlueGreenConfig.ActiveEnvironment); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "create_target", err)
		return status, err
	}

	// Setup service for active deployment
	// Use labels for the active deployment as selector
	activeLabels := map[string]string{
		"environment": request.BlueGreenConfig.ActiveEnvironment,
		"app": activeDeployment,
	}

	_, err = bg.serviceMgr.SetupService(ctx, request.Namespace, request.BlueGreenConfig.ServiceName, activeLabels)
	if err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "setup_service", err)
		return status, err
	}

	status.Metadata["active_deployment"] = activeDeployment
	status.Metadata["target_deployment"] = targetDeployment
	status.Metadata["service_name"] = request.BlueGreenConfig.ServiceName
	status.Status = "success"
	status.CurrentPhase = "completed"
	bg.addEvent(status, "info", "completed", "New blue-green deployment created")
	bg.storage.SaveDeployment(ctx, status)
	return status, nil
}

func (bg *BlueGreenStrategy) UpdateBlueGreenDeployment(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	status, err := bg.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment for update: %w", err)
	}

	status.CurrentPhase = "updating"
	status.Status = "in-progress"
	bg.addEvent(status, "info", "updating", "Starting blue-green deployment update")

	activeDeployment := status.Metadata["active_deployment"].(string)
	targetDeployment := status.Metadata["target_deployment"].(string)

	// Update target deployment with new image
	if err := bg.deploymentMgr.UpdateImage(ctx, request.Namespace, targetDeployment, request.NewImage); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "update_target", err)
		return status, err
	}

	// Wait for target ready
	if err := bg.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, targetDeployment, 5*time.Minute); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "wait_target_ready", err)
		return status, err
	}

	// Perform health check on target
	if request.HealthCheckConfig.Enabled {
		result, err := bg.healthMonitor.CheckDeploymentHealth(ctx, request.Namespace, targetDeployment, request.HealthCheckConfig)
		if err != nil || !result.Healthy {
			bg.updateStatusWithError(ctx, status, "failed", "health_check", fmt.Errorf("health check failed: %w", err))
			return status, err
		}
	}

	// Switch traffic to target
	if err := bg.serviceMgr.SwitchTrafficTo(ctx, request.Namespace, status.Metadata["service_name"].(string), targetDeployment); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "switch_traffic", err)
		return status, err
	}

	// Monitor deployment after switch
	if err := bg.monitorDeployment(ctx, request, status); err != nil {
		// If monitoring fails, rollback
		bg.serviceMgr.SwitchTrafficTo(ctx, request.Namespace, status.Metadata["service_name"].(string), activeDeployment)
		bg.updateStatusWithError(ctx, status, "failed", "monitor", err)
		return status, err
	}

	// Swap active and target
	status.Metadata["active_deployment"] = targetDeployment
	status.Metadata["target_deployment"] = activeDeployment

	status.Status = "success"
	status.CurrentPhase = "completed"
	status.EndTime = &[]time.Time{time.Now()}[0]
	bg.addEvent(status, "info", "completed", "Blue-green deployment update completed")
	bg.storage.SaveDeployment(ctx, status)
	return status, nil
}

func (bg *BlueGreenStrategy) createDeployment(ctx context.Context, namespace, name, image string, replicas int32, environment string) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": environment,
					"app": 	   name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"environment": environment,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: image,
						},
					},
				},
			},
		},
	}
	err := bg.kubeClient.CreateDeployment(ctx, deployment)
	if err != nil {
		return fmt.Errorf("failed to create deployment %s: %w", name, err)
	}
	return nil
}

//TODO: Implemnent MonitorDeployment method later.
func (bg *BlueGreenStrategy) monitorDeployment(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
	// Monitor logic remains the same as previous
	return nil
}

// Other methods (e.g., Rollback, GetStatus, addEvent, updateStatusWithError) remain the same as previous
func (bg *BlueGreenStrategy) addEvent(status *storage.DeploymentStatus, level, phase, message string) {
	status.Events = append(status.Events, storage.DeploymentEvent{
		Timestamp: time.Now(),
		Phase:     phase,
		Message:   message,
		Level:     level,
	})
	bg.logger.Info("event", zap.String("id", status.ID), zap.String("phase", phase), zap.String("message", message))
}

func (bg *BlueGreenStrategy) updateStatusWithError(ctx context.Context, status *storage.DeploymentStatus, statusValue, phase string, err error) {
	status.Status = statusValue
	status.CurrentPhase = phase
	status.Error = err.Error()
	status.EndTime = &[]time.Time{time.Now()}[0]
	bg.addEvent(status, "error", phase, err.Error())
	bg.storage.SaveDeployment(ctx,  status)
}

func (bg *BlueGreenStrategy) Rollback(ctx context.Context, deploymentName string) error {
	status, err := bg.storage.GetDeploymentByName(ctx, "", deploymentName)
	if err != nil {
		return fmt.Errorf("failed to get deployment status: %w", err)
	}

	// activeDeploymentName := status.Metadata["active_deployment"].(string)
	targetDeploymentName := status.Metadata["target_deployment"].(string)

	request := &types.DeploymentRequest{
		Namespace:        status.Namespace,
		AppName:             status.AppName,
		BlueGreenConfig:  &types.BlueGreenConfig{
			ServiceName: status.Metadata["service_name"].(string),
		},
	}

	if err := bg.serviceMgr.SwitchTrafficTo(ctx, request.Namespace, request.BlueGreenConfig.ServiceName, targetDeploymentName); err != nil {
		return fmt.Errorf("failed to rollback: %w", err)
	}

	status.Status = "rolled-back"
	status.EndTime = &[]time.Time{time.Now()}[0]
	bg.addEvent(status, "info", "rollback", "Rolled back to previous deployment")
	return bg.storage.SaveDeployment(ctx, status)
}

func (bg *BlueGreenStrategy) GetStatus(ctx context.Context, deploymentID string) (*storage.DeploymentStatus, error) {
	return bg.storage.GetDeployment(ctx, deploymentID)
}