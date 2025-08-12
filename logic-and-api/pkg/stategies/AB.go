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

type ABStrategy struct {
	kubeClient    *kubernetes.Client
	deploymentMgr *kubernetes.DeploymentManager
	ServiceMgr    *kubernetes.ServiceManager
	istioMgr      istio.IstioManager
	healthMonitor *monitoring.HealthMonitor
	storage       storage.Interface
	logger        *zap.Logger
}

func NewABStrategy(
	kubeClient *kubernetes.Client,
	istioMgr istio.IstioManager,
	ServiceMgr *kubernetes.ServiceManager,
	healthMonitor *monitoring.HealthMonitor,
	storage storage.Interface,
	logger *zap.Logger,
) *ABStrategy {
	return &ABStrategy{
		kubeClient:    kubeClient,
		deploymentMgr: kubernetes.NewDeploymentManager(kubeClient, logger),
		istioMgr:      istioMgr,
		ServiceMgr:    ServiceMgr,
		healthMonitor: healthMonitor,
		storage:       storage,
		logger:        logger,
	}
}

func (ab *ABStrategy) Name() string {
	return "ab"
}

func (ab *ABStrategy) Create(ctx context.Context, request *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error) {
	if err := ab.validateCreateRequest(request); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

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

	if err := ab.storage.SaveDeployment(ctx, status); err != nil {
		return nil, fmt.Errorf("failed to store deployment status: %w", err)
	}

	ab.addEvent(status, "info", "initializing", "Starting A/B deployment creation")

	// Check if deployment name is unique
	existing, err := ab.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil && !errors.IsNotFound(err) {
		ab.updateStatusWithError(ctx, status, "failed", "check_unique", err)
		return status, err
	}
	if existing != nil {
		ab.updateStatusWithError(ctx, status, "failed", "check_unique", fmt.Errorf("deployment name %s already exists", request.Name))
		return status, fmt.Errorf("deployment name %s already exists", request.Name)
	}

	// Create A and B deployments
	aDeploymentName := fmt.Sprintf("%s-a", request.Name)
	bDeploymentName := fmt.Sprintf("%s-b", request.Name)

	if err := ab.createDeployment(ctx, request.Namespace, aDeploymentName, request.Image, request.Replicas, "a"); err != nil {
		ab.updateStatusWithError(ctx, status, "failed", "create_a", err)
		return status, err
	}
	if err := ab.createDeployment(ctx, request.Namespace, bDeploymentName, request.Image, request.Replicas, "b"); err != nil {
		ab.updateStatusWithError(ctx, status, "failed", "create_b", err)
		return status, err
	}

	serviceName := request.CanaryConfig.ServiceName

	if request.CanaryConfig.ServiceName == "" {
		serviceName = fmt.Sprintf("%s-service", request.Name)
	}

	if err := ab.CreateService(ctx, request.Namespace, request.Name, serviceName); err != nil{
		ab.updateStatusWithError(ctx, status, "failed", fmt.Sprintf("create %s", serviceName), err)
		return status, err
	}

	if err := ab.setupIstioRouting(ctx, request, serviceName); err != nil {
		ab.updateStatusWithError(ctx, status, "failed", "setup_istio", err)
		return status, err
	}

	// Perform health check if enabled
	if request.HealthCheckConfig != nil && request.HealthCheckConfig.Enabled {
		if err := ab.performHealthCheck(ctx, request.Namespace, aDeploymentName, request.HealthCheckConfig); err != nil {
			ab.updateStatusWithError(ctx, status, "failed", "health_check_a", err)
			return status, err
		}
		if err := ab.performHealthCheck(ctx, request.Namespace, bDeploymentName, request.HealthCheckConfig); err != nil {
			ab.updateStatusWithError(ctx, status, "failed", "health_check_b", err)
			return status, err
		}
	}

	status.Metadata["a_deployment"] = aDeploymentName
	status.Metadata["b_deployment"] = bDeploymentName
	status.Metadata["service_name"] = request.ABConfig.ServiceName
	status.Metadata["routing_rules"] = request.ABConfig.RoutingRules
	status.Status = "success"
	status.CurrentPhase = "completed"
	ab.addEvent(status, "info", "completed", "A/B deployment created successfully")
	if err := ab.storage.SaveDeployment(ctx, status); err != nil {
		return status, fmt.Errorf("failed to save final status: %w", err)
	}
	return status, nil
}

func (ab *ABStrategy) Update(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	status, err := ab.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment for update: %w", err)
	}

	status.CurrentPhase = "updating"
	status.Status = "in-progress"
	ab.addEvent(status, "info", "updating", "Starting A/B deployment update")
	if err := ab.storage.SaveDeployment(ctx, status); err != nil {
		return status, fmt.Errorf("failed to save status: %w", err)
	}

	// Determine which variant to update (default to B for new image)
	bDeploymentName := status.Metadata["b_deployment"].(string)
	if err := ab.deploymentMgr.UpdateImage(ctx, request.Namespace, bDeploymentName, request.NewImage); err != nil {
		ab.updateStatusWithError(ctx, status, "failed", "update_b", err)
		return status, err
	}

	if err := ab.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, bDeploymentName, 5*time.Minute); err != nil {
		ab.updateStatusWithError(ctx, status, "failed", "wait_b_ready", err)
		return status, err
	}

	// Apply new routing rules if provided
	routingRules := status.Metadata["routing_rules"].([]types.RoutingRule)
	if request.ABConfig != nil && len(request.ABConfig.RoutingRules) > 0 {
		routingRules = request.ABConfig.RoutingRules
	}
	if err := ab.updateIstioRouting(ctx, request.Namespace, status.Metadata["service_name"].(string), routingRules); err != nil {
		ab.updateStatusWithError(ctx, status, "failed", "update_istio", err)
		return status, err
	}

	// Perform health check if enabled
	if request.HealthCheckConfig != nil && request.HealthCheckConfig.Enabled {
		if err := ab.performHealthCheck(ctx, request.Namespace, bDeploymentName, request.HealthCheckConfig); err != nil {
			ab.updateStatusWithError(ctx, status, "failed", "health_check_b", err)
			return status, err
		}
	}

	status.Metadata["routing_rules"] = routingRules
	status.Status = "success"
	status.CurrentPhase = "completed"
	status.EndTime = &[]time.Time{time.Now()}[0]
	ab.addEvent(status, "info", "completed", "A/B deployment updated successfully")
	if err := ab.storage.SaveDeployment(ctx, status); err != nil {
		return status, fmt.Errorf("failed to save final status: %w", err)
	}
	return status, nil
}

func (ab *ABStrategy) Rollback(ctx context.Context, request *types.RollbackRequest) error {
	status, err := ab.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil {
		return fmt.Errorf("failed to get deployment status: %w", err)
	}

	// Reset traffic to A variant
	routingRules := []types.RoutingRule{
		{HeaderKey: "x-ab-test", HeaderValue: "a", Variant: "a", Weight: 100},
	}
	if err := ab.updateIstioRouting(ctx, request.Namespace, status.Metadata["service_name"].(string), routingRules); err != nil {
		ab.logger.Warn("failed to reset routing during rollback", zap.Error(err))
	}

	// Scale down B variant
	if err := ab.kubeClient.ScaleDeployment(ctx, request.Namespace, status.Metadata["b_deployment"].(string), 0); err != nil {
		ab.logger.Warn("failed to scale down B variant during rollback", zap.Error(err))
	}

	status.Status = "rolled-back"
	status.EndTime = &[]time.Time{time.Now()}[0]
	ab.addEvent(status, "info", "rollback", fmt.Sprintf("Rolled back to A variant: %s", request.Reason))
	return ab.storage.SaveDeployment(ctx, status)
}

func (ab *ABStrategy) GetStatus(ctx context.Context, namespace, name string) (*storage.DeploymentStatus, error) {
	return ab.storage.GetDeploymentByName(ctx, namespace, name)
}

func (ab *ABStrategy) validateCreateRequest(request *types.DeploymentCreateRequest) error {
	if request.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if request.Name == "" {
		return fmt.Errorf("name is required")
	}
	if request.Image == "" {
		return fmt.Errorf("image is required")
	}
	if request.Replicas < 1 {
		return fmt.Errorf("replicas must be at least 1")
	}
	if request.Strategy != "ab" {
		return fmt.Errorf("strategy must be ab")
	}
	if request.ABConfig == nil {
		return fmt.Errorf("abConfig is required for A/B strategy")
	}
	if request.ABConfig.ServiceName == "" {
		return fmt.Errorf("serviceName is required in abConfig")
	}
	if len(request.ABConfig.RoutingRules) == 0 {
		return fmt.Errorf("routingRules must not be empty in abConfig")
	}
	totalWeight := 0
	for _, rule := range request.ABConfig.RoutingRules {
		if rule.Weight < 0 || rule.Weight > 100 {
			return fmt.Errorf("weight must be between 0 and 100")
		}
		totalWeight += rule.Weight
	}
	if totalWeight != 100 {
		return fmt.Errorf("total weight of routing rules must equal 100, got %d", totalWeight)
	}
	return nil
}

func (ab *ABStrategy) createDeployment(ctx context.Context, namespace, name, image string, replicas int32, variant string) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":     name,
					"variant": variant,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     name,
						"variant": variant,
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
	if err := ab.kubeClient.CreateDeployment(ctx, deployment); err != nil {
		return fmt.Errorf("failed to create %s deployment: %w", variant, err)
	}
	return ab.kubeClient.WaitForDeploymentReady(ctx, namespace, name, 5*time.Minute)
}

func (ab *ABStrategy) setupIstioRouting(ctx context.Context, request *types.DeploymentCreateRequest, ServiceName string) error {
	subsets := []istio.Subset{
		{Name: "a", Labels: map[string]string{"variant": "a"}},
		{Name: "b", Labels: map[string]string{"variant": "b"}},
	}
	if err := ab.istioMgr.CreateDestinationRule(ctx, request.Namespace, ServiceName, subsets); err != nil {
		return fmt.Errorf("failed to create destination rule: %w", err)
	}

	routes := make([]istio.Route, len(request.ABConfig.RoutingRules))
	for i, rule := range request.ABConfig.RoutingRules {
		routes[i] = istio.Route{
			Destination: istio.Destination{Host: ServiceName, Subset: rule.Variant},
			Weight:      rule.Weight,
			Match:       []istio.Match{{Name: rule.HeaderKey, Exact: rule.HeaderValue}},
		}
	}
	return ab.istioMgr.CreateVirtualService(ctx, request.Namespace, ServiceName, []string{ServiceName}, routes)
}

func (ab *ABStrategy) updateIstioRouting(ctx context.Context, namespace, serviceName string, routingRules []types.RoutingRule) error {
	routes := make([]istio.Route, len(routingRules))
	for i, rule := range routingRules {
		routes[i] = istio.Route{
			Destination: istio.Destination{Host: serviceName, Subset: rule.Variant},
			Weight:      rule.Weight,
			Match:       []istio.Match{{Name: rule.HeaderKey, Exact: rule.HeaderValue}},
		}
	}
	return ab.istioMgr.UpdateVirtualServiceRoutes(ctx, namespace, serviceName, routes)
}

func (ab *ABStrategy) performHealthCheck(ctx context.Context, namespace, deploymentName string, config *types.HealthCheckConfig) error {
	result, err := ab.healthMonitor.CheckDeploymentHealth(ctx, namespace, deploymentName, config)
	if err != nil || !result.Healthy {
		return fmt.Errorf("health check failed for %s: %w", deploymentName, err)
	}
	return nil
}

func (ab *ABStrategy) addEvent(status *storage.DeploymentStatus, level, phase, message string) {
	status.Events = append(status.Events, storage.DeploymentEvent{
		Timestamp: time.Now(),
		Phase:     phase,
		Message:   message,
		Level:     level,
	})
	ab.logger.Info("event", zap.String("id", status.ID), zap.String("phase", phase), zap.String("message", message))
}

func (ab *ABStrategy) updateStatusWithError(ctx context.Context, status *storage.DeploymentStatus, statusValue, phase string, err error) {
	status.Status = statusValue
	status.CurrentPhase = phase
	status.Error = err.Error()
	status.EndTime = &[]time.Time{time.Now()}[0]
	ab.addEvent(status, "error", phase, err.Error())
	ab.storage.SaveDeployment(ctx, status)
}

func(B *ABStrategy) CreateService(ctx context.Context, namespace, appName, serviceName string) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": appName,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
	if _, err := B.ServiceMgr.CreateService(ctx, service); err != nil {
		return fmt.Errorf("failed to create service %s: %w", serviceName, err)
	}
	return nil
}