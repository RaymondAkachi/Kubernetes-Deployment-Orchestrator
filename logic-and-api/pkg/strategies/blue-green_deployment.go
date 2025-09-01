package strategies

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

const (
	// Environments
	envBlue  = "blue"
	envGreen = "green"

	// Statuses
	statusPending   = "pending"
	statusInProgress = "in-progress"
	statusFailed    = "failed"
	statusSuccess   = "success"
	statusRolledBack = "rolled-back"

	// Phases
	phaseInitializing = "initializing"
	phaseUpdating     = "updating"
	phaseCompleted    = "completed"
)

type BlueGreenStrategy struct {
	kubeClient    *kubernetes.Client
	deploymentMgr *kubernetes.DeploymentManager
	serviceMgr    *kubernetes.ServiceManager
	healthMonitor *monitoring.HealthMonitor
	storage       storage.Interface
	logger        *zap.Logger
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

func (bg *BlueGreenStrategy) Validate(request *types.DeploymentCreateRequest) error {
	if request.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if request.Name == "" {
		return fmt.Errorf("name is required")
	}
	if request.ContainerSpec == nil || request.ContainerSpec.Image == "" {
		return fmt.Errorf("container spec with an image is required")
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
	if request.BlueGreenConfig.ServiceConfig == nil {
		return fmt.Errorf("serviceName is required in blueGreenConfig")
	}
	if request.BlueGreenConfig.ActiveEnvironment != envBlue && request.BlueGreenConfig.ActiveEnvironment != envGreen {
		return fmt.Errorf("activeEnvironment must be 'blue' or 'green'")
	}
	return nil
}

// func (bg *BlueGreenStrategy) Deploy(ctx context.Context, request *types.DeploymentRequest) (*types.DeploymentStatus, error) {
// 	// This method can be deprecated or used as a wrapper; direct calls to CreateBlueGreenDeployment or UpdateBlueGreenDeployment are preferred
// 	return nil, fmt.Errorf("use CreateBlueGreenDeployment or UpdateBlueGreenDeployment")
// }

func (bg *BlueGreenStrategy) CreateAppDeployment(ctx context.Context, request *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error) {
	deploymentID := uuid.New().String()

	status := &storage.DeploymentStatus{
		ID:            deploymentID,
		Namespace:     request.Namespace,
		Strategy:      request.Strategy,
		ServiceConfig: request.BlueGreenConfig.ServiceConfig,
		Replicas:      request.Replicas,
		AppName:       request.Name,
		ContainerSpec: request.ContainerSpec,
		Status:        statusPending,
		CurrentPhase:  phaseInitializing,
		StartTime:     time.Now(),
		Metadata:      make(map[string]interface{}),
		Events:        []storage.DeploymentEvent{},
		BlueGreenConfig: &storage.BlueGreenConfig{},
	}

	if err := bg.storage.CreateDeployment(ctx, status); err != nil {
		return nil, fmt.Errorf("failed to create initial deployment in database: %w", err)
	}

	bg.addEvent(status, "info", phaseInitializing, "Starting blue-green deployment creation")

	activeEnv := request.BlueGreenConfig.ActiveEnvironment
	targetEnv := envGreen
	if activeEnv == envGreen {
		targetEnv = envBlue
	}

	container := bg.buildContainer(request.Name, request.ContainerSpec, request.HealthCheckConfig)
	
	// Create both deployments concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 2)
	
	for _, env := range []string{activeEnv, targetEnv} {
		wg.Add(1)
		go func(environment string) {
			defer wg.Done()
			depName := fmt.Sprintf("%s-%s", request.Name, environment)
			// Scale the target (inactive) deployment to 0 initially
			replicas := request.Replicas
			if environment == targetEnv {
				replicas = 0
			}
			if err := bg.createDeployment(ctx, status, request.Namespace, request.Name, depName, replicas, environment, container); err != nil {
				errChan <- fmt.Errorf("failed to create %s deployment: %w", environment, err)
			}
		}(env)
	}
	
	wg.Wait()
	close(errChan)
	
	for err := range errChan {
		if err != nil {
			bg.updateStatusWithError(ctx, status, statusFailed, "create_deployments", err)
			return status, err
		}
	}

	activeLabels := map[string]string{"environment": activeEnv, "app": request.Name}
	if err := bg.CreateService(ctx, request.Namespace, request.Name, request.BlueGreenConfig.ServiceConfig.Name, request.ContainerSpec.Port, request.BlueGreenConfig.ServiceConfig, activeLabels); err != nil {
		bg.updateStatusWithError(ctx, status, statusFailed, "setup_service", err)
		return status, err
	}

	status.BlueGreenConfig.ActiveEnvironment = activeEnv
	status.BlueGreenConfig.TargetEnvironment = targetEnv
	status.BlueGreenConfig.ActiveDeploymentName = fmt.Sprintf("%s-%s", request.Name, activeEnv)
	status.BlueGreenConfig.TargetDeploymentName = fmt.Sprintf("%s-%s", request.Name, targetEnv)
	
	status.Status = statusSuccess
	status.CurrentPhase = phaseCompleted
	bg.addEvent(status, "info", phaseCompleted, "New blue-green deployment created")
	bg.storage.UpdateDeployment(ctx, status)
	return status, nil
}

func (bg *BlueGreenStrategy) UpdateAppDeployment(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	status, err := bg.storage.GetDeploymentByAppNamespace(ctx, request.Namespace, request.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve deployment status: %w", err)
	}

	status.CurrentPhase = phaseUpdating
	status.Status = statusInProgress
	bg.addEvent(status, "info", phaseUpdating, "Starting blue-green deployment update")

	// Determine configuration
	replicas := request.NewReplicas
	if replicas == 0 {
		replicas = status.Replicas
	}
	
	containerSpec := status.ContainerSpec
	if request.NewContainerSpec != nil && request.NewContainerSpec.Image != "" {
		containerSpec = request.NewContainerSpec
	}

	container := bg.buildContainer(status.AppName, containerSpec, request.NewHealthCheckConfig)
	
	targetDeployment := status.BlueGreenConfig.TargetDeploymentName
	activeDeployment := status.BlueGreenConfig.ActiveDeploymentName
	
	// Update and scale up target deployment
	if err := bg.deploymentMgr.UpdateDeploymentContainer(ctx, request.Namespace, targetDeployment, container); err != nil {
		bg.updateStatusWithError(ctx, status, statusFailed, "update_target", err)
		return status, err
	}
	if err := bg.kubeClient.ScaleDeployment(ctx, request.Namespace, targetDeployment, replicas); err != nil {
		bg.updateStatusWithError(ctx, status, statusFailed, "scale_up_target", err)
		return status, err
	}

	// Wait for target to be ready
	if err := bg.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, targetDeployment, 5*time.Minute); err != nil {
		bg.updateStatusWithError(ctx, status, statusFailed, "wait_target_ready", err)
		return status, err
	}

	// Health check the target before switching traffic
	if request.NewHealthCheckConfig != nil && request.NewHealthCheckConfig.Enabled {
		if result, err := bg.healthMonitor.CheckDeploymentHealth(ctx, request.Namespace, targetDeployment, request.NewHealthCheckConfig); err != nil || !result.Healthy {
			err := fmt.Errorf("health check failed for target deployment: %w", err)
			bg.updateStatusWithError(ctx, status, statusFailed, "health_check", err)
			return status, err
		}
	}

	// Switch traffic
	serviceName := status.ServiceConfig.Name
	if err := bg.serviceMgr.SwitchTrafficTo(ctx, request.Namespace, serviceName, targetDeployment); err != nil {
		bg.updateStatusWithError(ctx, status, statusFailed, "switch_traffic", err)
		return status, err
	}

	// Swap roles in status
	status.BlueGreenConfig.ActiveDeploymentName, status.BlueGreenConfig.TargetDeploymentName = status.BlueGreenConfig.TargetDeploymentName, status.BlueGreenConfig.ActiveDeploymentName
	status.BlueGreenConfig.ActiveEnvironment, status.BlueGreenConfig.TargetEnvironment = status.BlueGreenConfig.TargetEnvironment, status.BlueGreenConfig.ActiveEnvironment

	// Scale down the old active deployment
	if err := bg.kubeClient.ScaleDeployment(ctx, request.Namespace, activeDeployment, 0); err != nil {
		// Don't fail the whole operation for this, just log it.
		bg.logger.Warn("Failed to scale down old active deployment", zap.String("deployment", activeDeployment), zap.Error(err))
	}

	status.Status = statusSuccess
	status.CurrentPhase = phaseCompleted
	now := time.Now()
	status.EndTime = &now
	bg.addEvent(status, "info", phaseCompleted, "Blue-green deployment update completed")
	bg.storage.UpdateDeployment(ctx, status)
	return status, nil
}


func (bg *BlueGreenStrategy) createDeployment(ctx context.Context, status *storage.DeploymentStatus,  
	namespace, appname, deploymentName string, replicas int32, environment string, 
	container corev1.Container) error {

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": environment,
					"app":        appname,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"environment": environment,
						"app":        appname,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
				},
			},
		},
	}
	err := bg.kubeClient.CreateDeployment(ctx, deployment)
	if err != nil {
		return fmt.Errorf("failed to create deployment %s: %w", deploymentName, err)
	}

	for {
		// Use a timeout context for the polling loop to prevent it from hanging indefinitely
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for deployment %s to be ready: %w", deploymentName, ctx.Err())
		default:
			// Get the latest status of the deployment
			latestDeployment, err := bg.kubeClient.GetDeployment(ctx, namespace, deploymentName)
			if err != nil {
				return fmt.Errorf("failed to get deployment status: %w", err)
			}
			
			// Check if the desired number of replicas are ready
			if latestDeployment.Status.ReadyReplicas == replicas {
				// log.Printf("Deployment %s is ready with %d replicas.", deploymentName, replicas)
				bg.addEvent(status, "info", "deployment_check", "deployment is ready")

				return nil
			}
			
			// Wait before polling again
			time.Sleep(5 * time.Second)
		}

	}

}

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
	bg.storage.UpdateDeployment(ctx,  status)
}

func (bg *BlueGreenStrategy) Rollback(ctx context.Context, namespace, deploymentName string) (*storage.DeploymentStatus, error) {
	status, err := bg.storage.GetDeploymentByAppNamespace(ctx, namespace, deploymentName)
	if err != nil {
		return nil ,fmt.Errorf("failed to get deployment status: %w", err)
	}

	// The "target" deployment is the one we want to roll back to
	rollbackTarget := status.BlueGreenConfig.TargetDeploymentName
	
	// Ensure the rollback target is scaled up
	if err := bg.kubeClient.ScaleDeployment(ctx, status.Namespace, rollbackTarget, status.Replicas); err != nil {
		return nil, fmt.Errorf("failed to scale up rollback target deployment: %w", err)
	}

	// Wait for it to be ready
	if err := bg.kubeClient.WaitForDeploymentReady(ctx, status.Namespace, rollbackTarget, 5*time.Minute); err != nil {
		return nil, fmt.Errorf("rollback target deployment did not become ready: %w", err)
	}

	// Switch traffic back
	if err := bg.serviceMgr.SwitchTrafficTo(ctx, status.Namespace, status.ServiceConfig.Name, rollbackTarget); err != nil {
		return nil, fmt.Errorf("failed to switch traffic during rollback: %w", err)
	}

	// Swap roles back
	status.BlueGreenConfig.ActiveDeploymentName, status.BlueGreenConfig.TargetDeploymentName = status.BlueGreenConfig.TargetDeploymentName, status.BlueGreenConfig.ActiveDeploymentName
	status.BlueGreenConfig.ActiveEnvironment, status.BlueGreenConfig.TargetEnvironment = status.BlueGreenConfig.TargetEnvironment, status.BlueGreenConfig.ActiveEnvironment

	status.Status = statusRolledBack
	now := time.Now()
	status.EndTime = &now
	bg.addEvent(status, "info", "rollback", "Rolled back to previous active deployment")
	err =  bg.storage.UpdateDeployment(ctx, status)
	if err != nil{
		return nil, fmt.Errorf("there was an issue saving the final state to the db, %v", err)
	}

	return status, nil
}

func (bg *BlueGreenStrategy) GetStatus(ctx context.Context, deploymentID string) (*storage.DeploymentStatus, error) {
	return bg.storage.GetDeployment(ctx, deploymentID)
}

// func (bg *BlueGreenStrategy) monitorDeployment(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
// 	monitorDuration := 2 * time.Minute
// 	interval := 10 * time.Second
// 	start := time.Now()
// 	for {
// 		if request.NewHealthCheckConfig != nil && request.NewHealthCheckConfig.Enabled {
// 			if _, err := bg.healthMonitor.CheckDeploymentHealth(ctx, request.Namespace, request.Name, request.NewHealthCheckConfig); err != nil {
// 				return fmt.Errorf("monitoring health check failed: %w", err)
// 			}
// 		}
// 		if time.Since(start) > monitorDuration {
// 			break
// 		}
// 		time.Sleep(interval)
// 	}
// 	bg.addEvent(status, "info", "monitoring", "Deployment remained healthy during monitoring")
// 	return nil
// }

func (b *BlueGreenStrategy) CreateService(ctx context.Context, namespace, name, serviceName string, appPort int32,
	serviceConf *types.ServiceConfig, labels map[string]string) error {

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels:    labels,
		},
	}

	if serviceConf.Type != "ExternalName" {
		service.Spec = corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       serviceConf.Port,
					TargetPort: intstr.FromInt32(appPort),
				},
			},
		}
	}

	switch serviceConf.Type {
	case "ClusterIP":
		service.Spec.Type = corev1.ServiceTypeClusterIP

	case "NodePort":
		service.Spec.Type = corev1.ServiceTypeNodePort

	case "LoadBalancer":
		service.Spec.Type = corev1.ServiceTypeLoadBalancer

	case "ExternalName":
		service.Spec.Type = corev1.ServiceTypeExternalName
		service.Spec.ExternalName = serviceConf.ExternalName

	default:
		return fmt.Errorf("unsupported service type: %s", serviceConf.Type)
	}

	if _, err := b.serviceMgr.CreateService(ctx, service); err != nil {
		if errors.IsAlreadyExists(err) {
			b.logger.Info("service already exists, updating service", zap.String("service", serviceConf.Name))
			if _, err := b.serviceMgr.UpdateService(ctx, service); err != nil {
				b.logger.Info("failed to update service ", zap.String("service", serviceConf.Name))
			}
			return nil
		}
		b.logger.Error("An error occurred while creating service", zap.Error(err))
		return fmt.Errorf("error occurred while trying to create service: %w", err)
	}

	b.logger.Info("Service created successfully", zap.String("name", name), zap.String("type", string(service.Spec.Type)))

	return nil
}

func (bg *BlueGreenStrategy) buildContainer(appName string, spec *types.ContainerSpec, healthConfig *types.HealthCheckConfig) corev1.Container {
	container := corev1.Container{
		Name:  appName,
		Image: spec.Image,
		Ports: []corev1.ContainerPort{
			{ContainerPort: spec.Port},
		},
	}
	if healthConfig != nil && healthConfig.Enabled {
		if spec.LivenessProbe != nil{
			container.LivenessProbe = types.NewKubeProbe(spec.LivenessProbe)
		}
		if spec.ReadinessProbe != nil{
			container.ReadinessProbe = types.NewKubeProbe(spec.ReadinessProbe)
		}
	}
	
	return container
}