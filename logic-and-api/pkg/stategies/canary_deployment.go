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
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/istio"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes/monitoring"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type CanaryStrategy struct {
	kubeClient    *kubernetes.Client
	deploymentMgr *kubernetes.DeploymentManager
	istioMgr      istio.IstioManager
	healthMonitor *monitoring.HealthMonitor
	storage       storage.Interface
	ServiceMgr *kubernetes.ServiceManager
	logger        *zap.Logger
}

func NewCanaryStrategy(
	kubeClient *kubernetes.Client,
	istioMgr istio.IstioManager,
	healthMonitor *monitoring.HealthMonitor,
	storage storage.Interface,
	ServiceMgr *kubernetes.ServiceManager,
	logger *zap.Logger,
) *CanaryStrategy {
	return &CanaryStrategy{
		kubeClient:    kubeClient,
		deploymentMgr: kubernetes.NewDeploymentManager(kubeClient, logger),
		istioMgr:      istioMgr,
		healthMonitor: healthMonitor,
		storage:       storage,
		ServiceMgr: ServiceMgr,
		logger:        logger,
	}
}

func (c *CanaryStrategy) Name() string {
	return "canary"
}

func (c *CanaryStrategy) Validate(request *types.DeploymentRequest) error {
	// Validation code remains the same as previous
	return nil
}

func (c *CanaryStrategy) Deploy(ctx context.Context, request *types.DeploymentRequest) (*storage.DeploymentStatus, error) {
	// This method can be deprecated or used as a wrapper; direct calls to Create or Update are preferred
	return nil, fmt.Errorf("use CreateCanaryDeployment or UpdateCanaryDeployment")
}

func (c *CanaryStrategy) CreateCanaryDeployment(ctx context.Context, request *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error) {
	deploymentID := uuid.New().String()

	status := &storage.DeploymentStatus{
		ID:           deploymentID,
		Namespace:    request.Namespace,
		AppName:         request.Name,
		Status:       "pending",
		CurrentPhase: "initializing",
		StartTime:    time.Now(),
		Metadata:     make(map[string]interface{}),
		Events:       []storage.DeploymentEvent{},
	}

	if err := c.storage.SaveDeployment(ctx, status); err != nil {
		return nil, fmt.Errorf("failed to store deployment status: %w", err)
	}

	c.addEvent(status, "info", "initializing", "Starting canary deployment creation")

	// Check if deployment name is unique
	existing, err := c.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil && !errors.IsNotFound(err) {
		c.updateStatusWithError(ctx, status, "failed", "check_unique", err)
		return status, err
	}
	if existing != nil {
		c.updateStatusWithError(ctx, status, "failed", "check_unique", fmt.Errorf("deployment name %s already exists", request.Name))
		return status, fmt.Errorf("deployment name %s already exists", request.Name)
	}

	stableDeploymentName := fmt.Sprintf("%s-stable", request.Name)

	// Create stable deployment
	if err := c.createDeployment(ctx, request.Namespace, stableDeploymentName, request.Image, int32(request.Replicas), "stable"); err != nil {
		c.updateStatusWithError(ctx, status, "failed", "create_stable", err)
		return status, err
	}

	serviceName := request.CanaryConfig.ServiceName
	if serviceName == "" {
		serviceName = fmt.Sprintf("%s-service", request.Name)
	}

	//Create canary app service
	if err := c.createService(ctx, request.Namespace, request.Name, serviceName); err != nil {
		c.updateStatusWithError(ctx, status, "failed", "create_service", err)
		return status, err
	}

	// Setup Istio for stable
	if err := c.setupIstio(ctx, request.Namespace, serviceName); err != nil {
		c.updateStatusWithError(ctx, status, "failed", "setup_istio", err)
		return status, err
	}

	status.Metadata["canary_config"] = request.CanaryConfig
	status.Metadata["stable_deployment"] = stableDeploymentName
	status.Metadata["canary_deployment"] = fmt.Sprintf("%s-canary", request.Name)
	status.Metadata["service_name"] = serviceName
	status.Status = "success"
	status.CurrentPhase = "completed"
	c.addEvent(status, "info", "completed", "New canary deployment created")
	c.storage.SaveDeployment(ctx, status)
	return status, nil
}

func (c *CanaryStrategy) createDeployment(ctx context.Context, namespace, name, image string, replicas int32, version string) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"version": version,
					"app":    name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"version": version,
						"app": name,
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
	err := c.kubeClient.CreateDeployment(ctx, deployment)
	if err != nil {
		return fmt.Errorf("failed to create deployment %s: %w", name, err)
	}
	return nil
}

func (c *CanaryStrategy) setupIstio(ctx context.Context, namespace, serviceName string) error {
	subsets := []istio.Subset{
		{Name: "stable", Labels: map[string]string{"version": "stable"}},
		{Name: "canary", Labels: map[string]string{"version": "canary"}},
	}
	if err := c.istioMgr.CreateDestinationRule(ctx, namespace, serviceName, subsets); err != nil {
		return err
	}

	routes := []istio.Route{
		{Destination: istio.Destination{Host: serviceName, Subset: "stable"}, Weight: 100},
	}

	if err := c.istioMgr.CreateVirtualService(ctx, namespace, serviceName, []string{serviceName}, routes); err != nil {
		return err
	}
	return nil
}


func (c *CanaryStrategy) UpdateCanaryDeployment(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	status, err := c.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment for update: %w", err)
	}

	status.CurrentPhase = "updating"
	status.Status = "in-progress"
	c.addEvent(status, "info", "updating", "Starting canary deployment update")

	// Get canary config, override if provided
	config := request.CanaryConfig
	if config == nil {
		config = status.Metadata["canary_config"].(*types.CanaryConfig)
	}

	// status.Metadata["canary_config"] = config
	status.Metadata["current_traffic_percent"] = 0

	phases := []struct {
		name string
		fn   func(context.Context, *types.DeploymentUpdateRequest, *storage.DeploymentStatus, *types.CanaryConfig) error
	}{
		{"prepare_canary_update", c.prepareCanaryUpdate},
		{"wait_canary_ready", c.waitForCanaryReady},
		{"initial_health_check", c.performInitialHealthCheck},
		{"progressive_rollout", c.executeProgressiveRollout},
	}

	for _, phase := range phases {
		status.CurrentPhase = phase.name
		c.addEvent(status, "info", phase.name, fmt.Sprintf("Starting %s phase", phase.name))
		if err := phase.fn(ctx, request, status, config); err != nil {
			c.updateStatusWithError(ctx, status, "failed", phase.name, err)
			c.cleanupCanary(ctx, request, status)
			return status, err
		}
		c.storage.SaveDeployment(ctx, status)
	}

	if config.AutoPromote {
		if err := c.promoteCanaryUpdate(ctx, request, status); err != nil {
			c.updateStatusWithError(ctx, status, "failed", "promote_canary", err)
			return status, err
		}
	}

	status.Status = "success"
	status.CurrentPhase = "completed"
	status.EndTime = &[]time.Time{time.Now()}[0]
	c.addEvent(status, "info", "completed", "Canary deployment update completed")
	c.storage.SaveDeployment(ctx, status)
	return status, nil
}

func (c *CanaryStrategy) prepareCanaryUpdate(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus, config *types.CanaryConfig) error {
	canaryDeploymentName := status.Metadata["canary_deployment"].(string)

	// Create canary if not exists
	_, err := c.kubeClient.GetDeployment(ctx, request.Namespace, canaryDeploymentName)
	if errors.IsNotFound(err) {
		if err := c.createCanaryDeploymentUpdate(ctx, request, status); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("failed to check canary deployment: %w", err)
	}

	// Update image and scale
	if err := c.deploymentMgr.UpdateImage(ctx, request.Namespace, canaryDeploymentName, request.NewImage); err != nil {
		return fmt.Errorf("failed to update canary image: %w", err)
	}
	if err := c.kubeClient.ScaleDeployment(ctx, request.Namespace, canaryDeploymentName, request.Replicas); err != nil {
		return fmt.Errorf("failed to scale canary: %w", err)
	}

	weights := map[string]int{"stable": 100 - config.InitialTrafficPercent, "canary": config.InitialTrafficPercent}
	if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, status.Metadata["service_name"].(string), weights); err != nil {
		return fmt.Errorf("failed to set initial weights: %w", err)
	}

	status.Metadata["current_traffic_percent"] = config.InitialTrafficPercent
	c.addEvent(status, "info", "prepare_canary", fmt.Sprintf("Canary prepared with %d%% traffic", config.InitialTrafficPercent))
	return nil
}

func (c *CanaryStrategy) createCanaryDeploymentUpdate(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
	canaryDeploymentName := status.Metadata["canary_deployment"].(string)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      canaryDeploymentName,
			Namespace: request.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &request.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":     request.Name,
					"version": "canary",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     request.Name,
						"version": "canary",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  request.Name,
							Image: request.NewImage,
						},
					},
				},
			},
		},
	}
	err := c.kubeClient.CreateDeployment(ctx, deployment)
	if err != nil {
		return fmt.Errorf("failed to create canary deployment: %w", err)
	}
	return nil
}

func (c *CanaryStrategy) cleanupCanary(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) {
	weights := map[string]int{"stable": 100, "canary": 0}
	if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, status.Metadata["service_name"].(string), weights); err != nil {
		c.logger.Warn("failed to reset weights during cleanup", zap.Error(err))
	}
	if err := c.kubeClient.ScaleDeployment(ctx, request.Namespace, status.Metadata["canary_deployment"].(string), 0); err != nil {
		c.logger.Warn("failed to scale down canary during cleanup", zap.Error(err))
	}
}

func (c *CanaryStrategy) promoteCanaryUpdate(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
	if err := c.kubeClient.UpdateDepWithNamespace(ctx, request.Namespace, status.Metadata["stable_deployment"].(string), request.NewImage); err != nil {
		return fmt.Errorf("failed to update stable image: %w", err)
	}
	if err := c.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, status.Metadata["stable_deployment"].(string), 5*time.Minute); err != nil {
		return fmt.Errorf("stable not ready after promotion: %w", err)
	}

	weights := map[string]int{"stable": 100, "canary": 0}
	if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, status.Metadata["service_name"].(string), weights); err != nil {
		return fmt.Errorf("failed to reset weights: %w", err)
	}

	if err := c.kubeClient.ScaleDeployment(ctx, request.Namespace, status.Metadata["canary_deployment"].(string), 0); err != nil {
		c.logger.Warn("failed to scale down canary", zap.Error(err))
	}

	c.addEvent(status, "info", "promote", "Canary promoted to stable")
	return nil
}

func (c *CanaryStrategy) Rollback(ctx context.Context, deploymentName string) error {
	status, err := c.storage.GetDeploymentByName(ctx, "", deploymentName)
	if err != nil {
		return fmt.Errorf("failed to get deployment status: %w", err)
	}

	request := &types.DeploymentUpdateRequest{
		Namespace:        status.Namespace,
		Name:           status.AppName,
		CanaryConfig:     status.Metadata["canary_config"].(*types.CanaryConfig),
	}
	c.cleanupCanary(ctx, request, status)

	status.Status = "rolled-back"
	status.EndTime = &[]time.Time{time.Now()}[0]
	c.addEvent(status, "info", "rollback", "Rolled back to stable")
	return c.storage.SaveDeployment(ctx, status)
}

func (c *CanaryStrategy) GetStatus(ctx context.Context, deploymentID string) (*storage.DeploymentStatus, error) {
	return c.storage.GetDeployment(ctx, deploymentID)
}

func (c *CanaryStrategy) addEvent(status *storage.DeploymentStatus, level, phase, message string) {
	status.Events = append(status.Events, storage.DeploymentEvent{
		Timestamp: time.Now(),
		Phase:     phase,
		Message:   message,
		Level:     level,
	})
	c.logger.Info("event", zap.String("id", status.ID), zap.String("phase", phase), zap.String("message", message))
}

func (c *CanaryStrategy) updateStatusWithError(ctx context.Context, status *storage.DeploymentStatus, statusValue, phase string, err error) {
	status.Status = statusValue
	status.CurrentPhase = phase
	status.Error = err.Error()
	status.EndTime = &[]time.Time{time.Now()}[0]
	c.addEvent(status, "error", phase, err.Error())
	c.storage.SaveDeployment(ctx, status)
}

func (c *CanaryStrategy) createService(ctx context.Context, namespace, appName, serviceName string) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			// This selector matches all pods of the application, regardless of version
			Selector: map[string]string{
				"app": appName,
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	_, err := c.ServiceMgr.CreateService(ctx, service)
	if err != nil {
		return fmt.Errorf("failed to create service %s: %w", appName, err)
	}
	return nil
}


func (c *CanaryStrategy) waitForCanaryReady(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus, config *types.CanaryConfig) error {
	canaryDeploymentName := status.Metadata["canary_deployment"].(string)
	if err := c.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, canaryDeploymentName, 5*time.Minute); err != nil {
		return fmt.Errorf("canary not ready: %w", err)
	}
	c.addEvent(status, "info", "wait_ready", "Canary deployment is ready")
	return nil
}

func (c *CanaryStrategy) performInitialHealthCheck(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus, config *types.CanaryConfig) error {
	// if request.HealthCheck != "" {
	health := request.HealthCheckConfig
	if health == nil {
		return nil
	}

	canaryDeploymentName := status.Metadata["canary_deployment"].(string)
	result, err := c.healthMonitor.CheckDeploymentHealth(ctx, request.Namespace, canaryDeploymentName, health)
	if err != nil || !result.Healthy {
		return fmt.Errorf("initial health check failed: %w", err)
	}
	// }
	c.addEvent(status, "info", "initial_health_check", "Initial health check passed")
	return nil
}

func (c *CanaryStrategy) executeProgressiveRollout(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus, config *types.CanaryConfig) error {
	servicename := status.Metadata["service_name"].(string)
	health := request.HealthCheckConfig

	currentPercent := config.InitialTrafficPercent
	for currentPercent < config.MaxTrafficPercent {
		currentPercent += config.TrafficIncrement
		if currentPercent > config.MaxTrafficPercent {
			currentPercent = config.MaxTrafficPercent
		}

		weights := map[string]int{"stable": 100 - currentPercent, "canary": currentPercent}
		if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, servicename, weights); err != nil {
			return fmt.Errorf("failed to update weights to %d%%: %w", currentPercent, err)
		}

		status.Metadata["current_traffic_percent"] = currentPercent
		c.addEvent(status, "info", "rollout", fmt.Sprintf("Traffic shifted to %d%% canary", currentPercent))

		// if request.HealthCheck != "" {
			if err := c.waitAndCheckHealth(ctx, request, config, health, status); err != nil {
				return err
			}
		// } else {
		// 	time.Sleep(config.StepDuration)
		// }
	}
	return nil
}

func (c *CanaryStrategy) waitAndCheckHealth(ctx context.Context, request *types.DeploymentUpdateRequest,
	config *types.CanaryConfig, health *types.HealthCheckConfig,
	storage *storage.DeploymentStatus) error {

	canary_dep := storage.Metadata["canary_deployment"].(string)
	ctx, cancel := context.WithTimeout(ctx, config.StepDuration)
	defer cancel()

	ticker := time.NewTicker(config.AnalysisInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			result, err := c.healthMonitor.CheckDeploymentHealth(ctx, request.Namespace, canary_dep, health)
			if err != nil || !result.Healthy {
				return fmt.Errorf("health check failed: %w", err)
			}
		}
	}
}

// func (c *CanaryStrategy) promoteCanary(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
// 	stable_dep := status.Metadata["stable_deployment"].(string)
// 	canary_dep := status.Metadata["canary_deployment"].(string)
// 	service := status.Metadata["service_name"].(string)

// 	if err := c.deploymentMgr.UpdateImage(ctx, request.Namespace, stable_dep, request.NewImage); err != nil {
// 		return fmt.Errorf("failed to update stable image: %w", err)
// 	}
// 	if err := c.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, stable_dep, 5*time.Minute); err != nil {
// 		return fmt.Errorf("stable not ready after promotion: %w", err)
// 	}

// 	weights := map[string]int{"stable": 100, "canary": 0}
// 	if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, service, weights); err != nil {
// 		return fmt.Errorf("failed to reset weights: %w", err)
// 	}

// 	if err := c.kubeClient.ScaleDeployment(ctx, request.Namespace,canary_dep, 0); err != nil {
// 		c.logger.Warn("failed to scale down canary", zap.Error(err))
// 	}

// 	c.addEvent(status, "info", "promote", "Canary promoted to stable")
// 	status.Metadata["stable_deployment"] = stable_dep
// 	status.Metadata["canary_deployment"] = canary_dep
// 	return nil
// }
