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
	serviceMgr    *kubernetes.ServiceManager
	logger        *zap.Logger
}

func NewCanaryStrategy(
	kubeClient *kubernetes.Client,
	// deploymentMgr *kubernetes.DeploymentManager,
	istioMgr istio.IstioManager,
	healthMonitor *monitoring.HealthMonitor,
	storage storage.Interface,
	// serviceMgr *kubernetes.ServiceManager,
	logger *zap.Logger,
) *CanaryStrategy {
	return &CanaryStrategy{
		kubeClient:    kubeClient,
		deploymentMgr: kubernetes.NewDeploymentManager(kubeClient, logger),
		istioMgr:      istioMgr,
		healthMonitor: healthMonitor,
		storage:       storage,
		serviceMgr:    kubernetes.NewServiceManager(kubeClient, logger),
		logger:        logger,
	}
}

func (c *CanaryStrategy) Name() string {
	return "canary"
}

func (c *CanaryStrategy) CreateAppDeployment(ctx context.Context, request *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error) {
	deploymentID := uuid.New().String()

	status := &storage.DeploymentStatus{
		ID:           deploymentID,
		Namespace:    request.Namespace,
		Strategy:     request.Strategy,
		ServiceConfig: request.CanaryConfig.ServiceConfig,
		Replicas:    request.Replicas,
		AppName:       request.Name,
		Status:       "pending",
		CurrentPhase: "initializing",
		StartTime:    time.Now(),
		Metadata:     make(map[string]interface{}),
		Events:       []storage.DeploymentEvent{},
	}

	if err := c.storage.CreateDeployment(ctx, status); err != nil {
		return nil, fmt.Errorf("failed to store deployment status: %w", err)
	}

	c.addEvent(status, "info", "initializing", "Starting canary deployment creation")

	stableDeploymentName := fmt.Sprintf("%s-stable", request.Name)

	// Create stable deployment
	container := corev1.Container{
	Name:  request.Name,
	Image: request.ContainerSpec.Image,
	Ports: []corev1.ContainerPort{
		{
			ContainerPort: request.ContainerSpec.Port,
		},
	},
	}

	if request.HealthCheckConfig != nil{
		container.LivenessProbe = types.NewKubeProbe(request.ContainerSpec.LivenessProbe)
		container.ReadinessProbe = types.NewKubeProbe(request.ContainerSpec.ReadinessProbe)
	}

	if err := c.createDeployment(ctx, status, request.Namespace, request.Name, 
		stableDeploymentName, request.Replicas, "stable", container); err != nil {

		c.updateStatusWithError(ctx, status, "failed", "create_stable", err)
		return status, err
	}

	serviceName := request.CanaryConfig.ServiceConfig.Name
	if serviceName == "" {
		serviceName = fmt.Sprintf("%s-service", request.Name)
	}

	//Create canary app service
	labels := map[string]string{"app": request.Name}
	if err := c.CreateService(ctx, request.Namespace, request.Name, serviceName, request.ContainerSpec.Port,
		request.CanaryConfig.ServiceConfig, labels); err != nil {
		c.updateStatusWithError(ctx, status, "failed", "create_service", err)
		return status, err
	}

	// Setup Istio for stable
	subsets := []istio.Subset{
		{Name: "stable", Labels: map[string]string{"version": "stable"}},
		{Name: "canary", Labels: map[string]string{"version": "canary"}},
	}

	if err := c.setupIstio(ctx, request.Namespace, serviceName, subsets); err != nil {
		c.updateStatusWithError(ctx, status, "failed", "setup_istio", err)
		return status, err
	}

	status.CanaryConfig.StableDeploymentName = stableDeploymentName
	status.CanaryConfig.StableWeight = 100
	status.CanaryConfig.DeploymentConfig = request.CanaryConfig

	status.ContainerSpec = request.ContainerSpec
	status.Status = "success"
	status.CurrentPhase = "completed"
	c.addEvent(status, "info", "completed", "New canary deployment created")
	c.storage.UpdateDeployment(ctx, status)
	return status, nil
}

func (c *CanaryStrategy) createDeployment(ctx context.Context, status *storage.DeploymentStatus,  
	namespace, appname, deploymentName string, replicas int32, version string, 
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
					"version": version,
					"app":        appname,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"version": version,
						"app":        appname,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
				},
			},
		},
	}
	err := c.kubeClient.CreateDeployment(ctx, deployment)
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
			latestDeployment, err := c.kubeClient.GetDeployment(ctx, namespace, deploymentName)
			if err != nil {
				return fmt.Errorf("failed to get deployment status: %w", err)
			}
			
			// Check if the desired number of replicas are ready
			if latestDeployment.Status.ReadyReplicas == replicas {
				// log.Printf("Deployment %s is ready with %d replicas.", deploymentName, replicas)
				c.addEvent(status, "info", "deployment_check", "deployment is ready")

				return nil
			}
			
			// Wait before polling again
			time.Sleep(5 * time.Second)
		}

	}

}

func (c *CanaryStrategy) setupIstio(ctx context.Context, namespace, serviceName string, subsets []istio.Subset) error {
	
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


func (c *CanaryStrategy) UpdateAppDeployment(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	status, err := c.storage.GetDeploymentByAppNamespace(ctx, request.Namespace, request.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment for update: %w", err)
	}

	
	status.CurrentPhase = "updating"
	status.Status = "in-progress"
	c.addEvent(status, "info", "updating", "Starting canary deployment update")


	status.CanaryConfig.CanaryWeight = 0
	config := status.CanaryConfig.DeploymentConfig
	canaryDeploymentName := fmt.Sprintf("%s-canary", request.Name)    

	phases := []struct {
		name string
		fn   func(context.Context,  *types.DeploymentUpdateRequest, string, *storage.DeploymentStatus, *types.CanaryConfig) error
	}{
		{"prepare_canary_update", c.prepareCanaryUpdate},
		{"wait_canary_ready", c.waitForCanaryReady},
		{"initial_health_check", c.performInitialHealthCheck},
		{"progressive_rollout", c.executeProgressiveRollout},
	}

	for _, phase := range phases {
		status.CurrentPhase = phase.name
		c.addEvent(status, "info", phase.name, fmt.Sprintf("Starting %s phase", phase.name))
		if err := phase.fn(ctx, request, canaryDeploymentName, status, config); err != nil {
			c.updateStatusWithError(ctx, status, "failed", phase.name, err)
			if err = c.cleanupCanary(ctx, request.Namespace, status.ServiceConfig.Name, canaryDeploymentName, status); err != nil{
				return nil, fmt.Errorf("an operation failed during update, attempted rollback failed %v", err )
			}
			return status, err
		}
		c.storage.UpdateDeployment(ctx, status)
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
	c.storage.UpdateDeployment(ctx, status)
	return status, nil
}

func (c *CanaryStrategy) prepareCanaryUpdate(ctx context.Context, request *types.DeploymentUpdateRequest, 
	canaryName string, status *storage.DeploymentStatus, config *types.CanaryConfig) error {
	
	replicas := request.NewReplicas
	if replicas == 0{
		replicas = status.Replicas
	}


	serviceConf := status.ServiceConfig
	// serviceName := serviceConf.Name
	if request.NewCanaryConfig.ServiceConfig != nil && request.NewCanaryConfig.ServiceConfig.Name != ""{
		// serviceName = request.CanaryConfig.ServiceConfig.Name
		serviceConf = request.NewCanaryConfig.ServiceConfig
	}
	
	container_spec := status.ContainerSpec
	container := corev1.Container{
		Name: status.AppName,
		Image: status.ContainerSpec.Image,
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: status.ContainerSpec.Port,
			},
		},
	}
	container.LivenessProbe = types.NewKubeProbe(status.ContainerSpec.LivenessProbe)
	container.ReadinessProbe = types.NewKubeProbe(status.ContainerSpec.ReadinessProbe)

	if request.NewContainerSpec != nil && request.NewContainerSpec.Image != ""{
		container_spec = request.NewContainerSpec
		container = corev1.Container{
		Name:  request.Name,
		Image: request.NewContainerSpec.Image,
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: request.NewContainerSpec.Port,
			},
		},
		}

		if request.NewHealthCheckConfig != nil{
			container.LivenessProbe = types.NewKubeProbe(request.NewContainerSpec.LivenessProbe)
			container.ReadinessProbe = types.NewKubeProbe(request.NewContainerSpec.ReadinessProbe)
		}
	}

	_, err := c.serviceMgr.GetService(ctx, request.Namespace, serviceConf.Name)
	if err != nil && errors.IsNotFound(err) {
		labels := map[string]string{"app":request.Name}
		if err = c.CreateService(ctx, request.Namespace, request.Name, serviceConf.Name, status.ContainerSpec.Port,
			serviceConf, labels); err != nil{

			c.updateStatusWithError(ctx, status, "failed", "new service creation failed", err)
			return err
		}
	}
	
	// Create canary if not exists
	_, err = c.kubeClient.GetDeployment(ctx, request.Namespace, canaryName)
	if errors.IsNotFound(err) {
		if err := c.createDeployment(ctx, status, request.Namespace, request.Name, canaryName,
			replicas, "canary", container); err != nil {
			return err
		}

	} else if err != nil {
		return fmt.Errorf("failed to check canary deployment: %w", err)
	}


	if err := c.deploymentMgr.UpdateDeploymentContainer(ctx, request.Namespace, canaryName, container); err != nil {
		return fmt.Errorf("failed to update canary image: %w", err)
	}

	weights := map[string]int{"stable": 100 - int(config.InitialTrafficPercent), "canary": int(config.InitialTrafficPercent)}
	if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, serviceConf.Name, weights); err != nil {
		return fmt.Errorf("failed to set initial weights: %w", err)
	}
	

	status.CanaryConfig.CanaryImage = container.Image
	status.Metadata["canary_container_spec"] = container_spec
	status.ServiceConfig = serviceConf
	status.CanaryConfig.CanaryWeight = config.InitialTrafficPercent
	if err := c.storage.UpdateDeployment(ctx, status); err != nil {
		c.updateStatusWithError(ctx, status, "failed", "failed to save deployment status", err)
		return fmt.Errorf("failed to save deployment status: %w", err)
	}
	c.addEvent(status, "info", "prepare_canary", fmt.Sprintf("Canary prepared with %v traffic", config.InitialTrafficPercent))
	return nil
}


func (c *CanaryStrategy) cleanupCanary(ctx context.Context, namespace, serviceName, canaryDeployment string, status *storage.DeploymentStatus) error{
	weights := map[string]int{"stable": 100, "canary": 0}
	if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, namespace, serviceName, weights); err != nil {
		c.logger.Warn("failed to reset weights during cleanup", zap.Error(err))
		return fmt.Errorf("failed to reset weights during cleanup %v", err)
	}
	if err := c.kubeClient.ScaleDeployment(ctx, namespace, canaryDeployment, 0); err != nil {
		c.logger.Warn("failed to scale down canary during cleanup", zap.Error(err))
		return fmt.Errorf("failed to reset weights during cleanup %v", err)
	}
	status.CanaryConfig.CanaryWeight = 0
	
	return nil
}

func (c *CanaryStrategy) promoteCanaryUpdate(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
	spec, ok := status.Metadata["canary_container_spec"].(*types.ContainerSpec)
	if !ok {
		return fmt.Errorf("canary container spec not found in metadata or is of the wrong type")
	}

	container := c.buildContainer(request.Name, spec, status.HealthCheck)
	
	if err := c.deploymentMgr.UpdateDeploymentContainer(ctx, request.Namespace, status.CanaryConfig.StableDeploymentName, container); err != nil {
		return fmt.Errorf("failed to update stable image: %w", err)
	}
	if err := c.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, status.CanaryConfig.StableDeploymentName, 5*time.Minute); err != nil {
		return fmt.Errorf("stable not ready after promotion: %w", err)
	}

	weights := map[string]int{"stable": 100, "canary": 0}
	if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, status.ServiceConfig.Name, weights); err != nil {
		return fmt.Errorf("failed to reset weights: %w", err)
	}

	if err := c.kubeClient.ScaleDeployment(ctx, request.Namespace, status.CanaryConfig.CanaryDeploymentName, 0); err != nil {
		c.logger.Warn("failed to scale down canary", zap.Error(err))
	}

	c.addEvent(status, "info", "promote", "Canary promoted to stable")

	status.CanaryConfig.StableWeight = 100
	status.CanaryConfig.CanaryWeight = 0
	status.ContainerSpec = spec
	status.Status = "success"
	status.CurrentPhase = "completed"
	now := time.Now()
	status.EndTime = &now
	if err := c.storage.UpdateDeployment(ctx, status); err != nil {
		c.updateStatusWithError(ctx, status, "failed", "save_after_promotion", err)
		return fmt.Errorf("failed to save deployment status after promotion: %w", err)
	}
	return nil
}

func (c *CanaryStrategy) Rollback(ctx context.Context, namespace, deploymentName string) (*storage.DeploymentStatus, error) {
	status, err := c.storage.GetDeploymentByAppNamespace(ctx, namespace, deploymentName)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment status: %w", err)
	}

	if err = c.cleanupCanary(ctx, status.Namespace, status.ServiceConfig.Name, status.CanaryConfig.CanaryDeploymentName, status); err != nil {
		return nil, fmt.Errorf("An error occurred during rollback %v", err)
	}

	status.Status = "rolled back"
	now := time.Now()
	status.EndTime = &now
	c.addEvent(status, "info", "rollback", "Rolled back to stable")
	if err =  c.storage.UpdateDeployment(ctx, status); err != nil {
		return nil, fmt.Errorf("an issue occurred while saving application state %v", err)
	}

	return status, nil
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
	c.storage.UpdateDeployment(ctx, status)
}

func (c *CanaryStrategy) CreateService(ctx context.Context, namespace, name, serviceName string, appPort int32,
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

	if _, err := c.serviceMgr.CreateService(ctx, service); err != nil {
		if errors.IsAlreadyExists(err) {
			c.logger.Info("service already exists, updating service", zap.String("service", serviceConf.Name))
			if _, err := c.serviceMgr.UpdateService(ctx, service); err != nil {
				c.logger.Info("failed to update service ", zap.String("service", serviceConf.Name))
			}
			return nil
		}
		c.logger.Error("An error occurred while creating service", zap.Error(err))
		return fmt.Errorf("error occurred while trying to create service: %w", err)
	}

	c.logger.Info("Service created successfully", zap.String("name", name), zap.String("type", string(service.Spec.Type)))

	return nil
}

func (c *CanaryStrategy) waitForCanaryReady(ctx context.Context, request *types.DeploymentUpdateRequest, 
	canaryName string, status *storage.DeploymentStatus, config *types.CanaryConfig) error {

	if err := c.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, canaryName, 5*time.Minute); err != nil {
		return fmt.Errorf("canary not ready: %w", err)
	}
	c.addEvent(status, "info", "wait_ready", "Canary deployment is ready")
	return nil
}

func (c *CanaryStrategy) performInitialHealthCheck(ctx context.Context, request *types.DeploymentUpdateRequest, 
	canaryName string, status *storage.DeploymentStatus, config *types.CanaryConfig) error {
	
	health := request.NewHealthCheckConfig
	if health == nil {
		health = status.HealthCheck
	}

	result, err := c.healthMonitor.CheckDeploymentHealth(ctx, request.Namespace, canaryName, health)
	if err != nil || !result.Healthy {
		return fmt.Errorf("initial health check failed: %w", err)
	}
	// }
	c.addEvent(status, "info", "initial_health_check", "Initial health check passed")
	return nil
}

func (c *CanaryStrategy) executeProgressiveRollout(ctx context.Context, request *types.DeploymentUpdateRequest, 
	canaryName string, status *storage.DeploymentStatus, config *types.CanaryConfig) error {

	servicename := status.ServiceConfig.Name

	health := status.HealthCheck
	if request.NewHealthCheckConfig != nil{
		health = request.NewHealthCheckConfig
	}

	currentPercent := config.InitialTrafficPercent

	for currentPercent < config.MaxTrafficPercent {
		currentPercent += config.TrafficIncrement
		if currentPercent > config.MaxTrafficPercent {
			currentPercent = config.MaxTrafficPercent
		}

		weights := map[string]int{"stable": 100 - int(currentPercent), "canary": int(currentPercent)}
		if err := c.istioMgr.UpdateVirtualServiceWeights(ctx, request.Namespace, servicename, weights); err != nil {
			return fmt.Errorf("failed to update weights to %d%%: %w", currentPercent, err)
		}

		status.CanaryConfig.CanaryWeight = currentPercent
		c.addEvent(status, "info", "rollout", fmt.Sprintf("Traffic shifted to %d%% canary", currentPercent))

		// if request.HealthCheck != "" {
		if request.NewHealthCheckConfig != nil {
			if err := c.waitAndCheckHealth(ctx, request, config, health, status); err != nil {
				return err
			}
			
		}
			
		time.Sleep(config.StepDuration)
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

func (c *CanaryStrategy) buildContainer(appName string, spec *types.ContainerSpec, healthConfig *types.HealthCheckConfig) corev1.Container {
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

// func (c *CanaryStrategy) createCanaryService(ctx context.Context, namespace, serviceName, appName string) (error) {
// 	service_data := &corev1.Service{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      serviceName,
// 			Namespace: namespace,
// 		},
// 		Spec: corev1.ServiceSpec{
// 			// This selector matches all pods of the application, regardless of version
// 			Selector: map[string]string{
// 				"app": appName,
// 			},
// 			Ports: []corev1.ServicePort{
// 				{
// 					Protocol:   corev1.ProtocolTCP,
// 					Port:       80,
// 					TargetPort: intstr.FromInt(8080),
// 				},
// 			},
// 			Type: corev1.ServiceTypeClusterIP,
// 		},
// 	}

// 	_, err :=  c.ServiceMgr.CreateService(ctx, service_data)
// 	if err != nil{
// 		return fmt.Errorf("failed to create service named %s", serviceName)
// 	}

// 	return nil
// }

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
