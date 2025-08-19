package strategies

import (
	"context"
	"fmt"

	// "log"
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

//TODO: IMplement validate method for create and updates requests later
func (bg *BlueGreenStrategy) Validate(request *types.DeploymentCreateRequest) error {
	if request.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if request.Name == "" {
		return fmt.Errorf("name is required")
	}
	if request.Image == "" {
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
	if request.ServiceName == "" {
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
		Strategy:     request.Strategy,
		ServiceName: request.ServiceName,
		Replicas:    request.Replicas,
		AppName:       request.Name,
		Image:        request.Image,
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
	
	activeEnvironment := "blue"
	targetEnvironment := "blue"

	activeDeployment := blueDeploymentName
	targetDeployment := greenDeploymentName
	if request.BlueGreenConfig.ActiveEnvironment == "green" {
		activeDeployment = greenDeploymentName
		targetDeployment = blueDeploymentName

		activeEnvironment = "green"
		targetEnvironment = "blue"
		
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// channel to receive errors from the goroutines
	errChan := make(chan error, 2)

	// Goroutine for creating the 'active deployment
	go func() {
		defer wg.Done()
		if err := bg.createDeployment(ctx, status, request.Namespace, request.Name,
			activeDeployment, request.Image, request.Replicas, activeDeployment, request.HealthCheckConfig); err != nil {
			errChan <- fmt.Errorf("failed to create active deployment: %w", err)
		}
	}()

	// Goroutine for creating the 'target' deployment
	go func() {
		defer wg.Done()
		if err := bg.createDeployment(ctx, status, request.Namespace, request.Name,
			targetDeployment, request.Image, request.Replicas, targetEnvironment, request.HealthCheckConfig); err != nil {
			errChan <- fmt.Errorf("failed to create target deployment: %w", err)
		}
	}()

	// Wait for both goroutines to finish
	wg.Wait()
	close(errChan)

	// Check for any errors that occurred
	for err := range errChan {
		if err != nil {
			bg.updateStatusWithError(ctx, status, "failed", "create_deployments", err)
			return status, err
		}
	}

	// Setup service for active deployment
	// Use labels for the active deployment as selector
	activeLabels := map[string]string{
		"environment": activeEnvironment,
		"app": request.Name,
	}

	_, err = bg.serviceMgr.SetupService(ctx, request.Namespace, request.ServiceName, activeLabels)
	if err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "setup_service", err)
		return status, err
	}

	status.ServiceName = request.ServiceName
	status.BlueGreenConfig.ActiveEnvironment = activeEnvironment
	status.BlueGreenConfig.TargetEnvironment = targetEnvironment

	status.BlueGreenConfig.TargetDeploymentName = targetDeployment
	status.BlueGreenConfig.ActiveDeploymentName = activeDeployment
	
	status.Status = "success"
	status.CurrentPhase = "completed"
	bg.addEvent(status, "info", "completed", "New blue-green deployment created")
	bg.storage.SaveDeployment(ctx, status)
	return status, nil
}

func (bg *BlueGreenStrategy) UpdateBlueGreenDeployment(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	status, err := bg.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil {
		return status, fmt.Errorf("an error occured while trying to retrive app name: %v", err)
	}

	replicas := request.Replicas
	if replicas == 0 {
		replicas = status.Replicas
	}

	serviceName := status.ServiceName

	if request.BlueGreenConfig.NewServiceName != ""{
		serviceName = request.BlueGreenConfig.NewServiceName

		if _, err := bg.serviceMgr.GetService(ctx, request.Namespace, serviceName); err != nil{
			labels := map[string]string{"app": request.Name, "environment":status.BlueGreenConfig.TargetDeploymentName}
			if err = bg.CreateService(ctx, request.Namespace, request.Name, serviceName, labels); err != nil{
				bg.updateStatusWithError(ctx, status, "failed to create new service", "upate deployment", err)
				return nil, err
			}
		}
	}

	activeDeployment := status.BlueGreenConfig.ActiveDeploymentName
	targetDeployment := status.BlueGreenConfig.TargetDeploymentName
	targetEnvironment := status.BlueGreenConfig.TargetEnvironment
	activeEnviroment := status.BlueGreenConfig.ActiveEnvironment

	
	status.CurrentPhase = "updating"
	status.Status = "in-progress"
	bg.addEvent(status, "info", "updating", "Starting blue-green deployment update")


	// Update target deployment with new image
	if err := bg.deploymentMgr.UpdateImage(ctx, request.Namespace, targetDeployment, request.NewImage); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "update_target", err)
		return status, err
	}

	if err := bg.kubeClient.ScaleDeployment(ctx, request.Namespace, targetDeployment, replicas); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "update_target_replicas", err)
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
	if err := bg.serviceMgr.SwitchTrafficTo(ctx, request.Namespace, serviceName, targetDeployment); err != nil {
		bg.updateStatusWithError(ctx, status, "failed", "switch_traffic", err)
		return status, err
	}

	// Monitor deployment after switch
	if err := bg.monitorDeployment(ctx, request, status); err != nil {
		// If monitoring fails, rollback
		bg.serviceMgr.SwitchTrafficTo(ctx, request.Namespace, serviceName, activeDeployment)
		bg.updateStatusWithError(ctx, status, "failed", "monitor", err)
		return status, err
	}

	// Swap active and target
	status.BlueGreenConfig.ActiveDeploymentName = targetDeployment
	status.BlueGreenConfig.TargetDeploymentName = activeDeployment
	status.BlueGreenConfig.ActiveEnvironment = targetEnvironment
	status.BlueGreenConfig.TargetEnvironment = activeEnviroment

	status.Status = "success"
	status.CurrentPhase = "completed"
	status.EndTime = &[]time.Time{time.Now()}[0]
	bg.addEvent(status, "info", "completed", "Blue-green deployment update completed")
	bg.storage.SaveDeployment(ctx, status)
	return status, nil
}


func (bg *BlueGreenStrategy) createDeployment(ctx context.Context, status *storage.DeploymentStatus,  
	namespace, appname, deploymentName, image string, replicas int32, environment string, 
	healthCheckConfig *types.HealthCheckConfig) error {
	
	container := corev1.Container{
		Name:  appname,
		Image: image,
	}

	if healthCheckConfig != nil && healthCheckConfig.Enabled {
		container.LivenessProbe = types.NewKubeProbe(healthCheckConfig.LivenessProbe)
		container.ReadinessProbe = types.NewKubeProbe(healthCheckConfig.ReadinessProbe)
	}

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
	bg.storage.SaveDeployment(ctx,  status)
}

func (bg *BlueGreenStrategy) Rollback(ctx context.Context, deploymentName string) error {
	status, err := bg.storage.GetDeploymentByName(ctx, "", deploymentName)
	if err != nil {
		return fmt.Errorf("failed to get deployment status: %w", err)
	}

	// activeDeploymentName := status.Metadata["active_deployment"].(string)
	activeDeployment := status.BlueGreenConfig.ActiveDeploymentName
	targetDeployment := status.BlueGreenConfig.TargetDeploymentName
	targetEnvironment := status.BlueGreenConfig.TargetEnvironment
	activeEnviroment := status.BlueGreenConfig.ActiveEnvironment

	namespace := status.Namespace
	serviceName := status.ServiceName

	if err := bg.serviceMgr.SwitchTrafficTo(ctx, namespace, serviceName, targetDeployment); err != nil {
		return fmt.Errorf("failed to rollback: %w", err)
	}

	status.BlueGreenConfig.ActiveDeploymentName = targetDeployment
	status.BlueGreenConfig.TargetDeploymentName = activeDeployment
	status.BlueGreenConfig.ActiveEnvironment = targetEnvironment
	status.BlueGreenConfig.TargetEnvironment = activeEnviroment

	status.Status = "rolled-back"
	status.EndTime = &[]time.Time{time.Now()}[0]
	bg.addEvent(status, "info", "rollback", "Rolled back to previous deployment")
	return bg.storage.SaveDeployment(ctx, status)
}

func (bg *BlueGreenStrategy) GetStatus(ctx context.Context, deploymentID string) (*storage.DeploymentStatus, error) {
	return bg.storage.GetDeployment(ctx, deploymentID)
}

func (bg *BlueGreenStrategy) monitorDeployment(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
	monitorDuration := 2 * time.Minute
	interval := 10 * time.Second
	start := time.Now()
	for {
		if request.HealthCheckConfig != nil && request.HealthCheckConfig.Enabled {
			if _, err := bg.healthMonitor.CheckDeploymentHealth(ctx, request.Namespace, request.Name, request.HealthCheckConfig); err != nil {
				return fmt.Errorf("monitoring health check failed: %w", err)
			}
		}
		if time.Since(start) > monitorDuration {
			break
		}
		time.Sleep(interval)
	}
	bg.addEvent(status, "info", "monitoring", "Deployment remained healthy during monitoring")
	return nil
}

func (b *BlueGreenStrategy) CreateService(ctx context.Context, namespace, appName, serviceName string, labels map[string]string) error {
	
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	if _, err := b.serviceMgr.CreateService(ctx, service); err != nil {
		// Check if service already exists
		if errors.IsAlreadyExists(err) {
			b.logger.Info("service already exists, skipping creation", zap.String("service", serviceName))
			return nil
		}
		return fmt.Errorf("failed to create service %s: %w", serviceName, err)
	}
	return nil
}
