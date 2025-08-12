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

type FeatureFlagStrategy struct {
	kubeClient    *kubernetes.Client
	deploymentMgr *kubernetes.DeploymentManager
	configMapMgr  *kubernetes.ConfigMapManager
	healthMonitor *monitoring.HealthMonitor
	storage       storage.Interface
	logger        *zap.Logger
}

func NewFeatureFlagStrategy(
	kubeClient *kubernetes.Client,
	healthMonitor *monitoring.HealthMonitor,
	storage storage.Interface,
	logger *zap.Logger,
) *FeatureFlagStrategy {
	return &FeatureFlagStrategy{
		kubeClient:    kubeClient,
		deploymentMgr: kubernetes.NewDeploymentManager(kubeClient, logger),
		configMapMgr:  kubernetes.NewConfigMapManager(kubeClient, logger),
		healthMonitor: healthMonitor,
		storage:       storage,
		logger:        logger,
	}
}

func (ff *FeatureFlagStrategy) Name() string {
	return "feature-flag"
}

func (ff *FeatureFlagStrategy) Create(ctx context.Context, request *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error) {
	if err := ff.validateCreateRequest(request); err != nil {
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

	if err := ff.storage.SaveDeployment(ctx, status); err != nil {
		return nil, fmt.Errorf("failed to store deployment status: %w", err)
	}

	ff.addEvent(status, "info", "initializing", "Starting feature-flag deployment creation")

	// Check if deployment name is unique
	existing, err := ff.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil && !errors.IsNotFound(err) {
		ff.updateStatusWithError(ctx, status, "failed", "check_unique", err)
		return status, err
	}
	if existing != nil {
		ff.updateStatusWithError(ctx, status, "failed", "check_unique", fmt.Errorf("deployment name %s already exists", request.Name))
		return status, fmt.Errorf("deployment name %s already exists", request.Name)
	}

	// Create primary deployment
	deploymentName := request.Name
	if err := ff.createDeployment(ctx, request.Namespace, deploymentName, request.Image, request.Replicas); err != nil {
		ff.updateStatusWithError(ctx, status, "failed", "create_deployment", err)
		return status, err
	}

	// Create ConfigMap for feature flags
	if err := ff.createConfigMap(ctx, request.Namespace, request.FeatureFlagConfig.ConfigMapName, request.FeatureFlagConfig.Flags); err != nil {
		ff.updateStatusWithError(ctx, status, "failed", "create_configmap", err)
		return status, err
	}

	// Perform health check if enabled
	if request.HealthCheckConfig != nil && request.HealthCheckConfig.Enabled {
		if err := ff.performHealthCheck(ctx, request.Namespace, deploymentName, request.HealthCheckConfig); err != nil {
			ff.updateStatusWithError(ctx, status, "failed", "health_check", err)
			return status, err
		}
	}

	// Set initial rollout percentage
	status.Metadata["configmap_name"] = request.FeatureFlagConfig.ConfigMapName
	status.Metadata["rollout_percent"] = request.FeatureFlagConfig.RolloutPercent
	status.Metadata["flags"] = request.FeatureFlagConfig.Flags
	status.Status = "success"
	status.CurrentPhase = "completed"
	ff.addEvent(status, "info", "completed", "Feature-flag deployment created successfully")
	if err := ff.storage.SaveDeployment(ctx, status); err != nil {
		return status, fmt.Errorf("failed to save final status: %w", err)
	}
	return status, nil
}

func (ff *FeatureFlagStrategy) Update(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error) {
	status, err := ff.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment for update: %w", err)
	}

	status.CurrentPhase = "updating"
	status.Status = "in-progress"
	ff.addEvent(status, "info", "updating", "Starting feature-flag deployment update")
	if err := ff.storage.SaveDeployment(ctx, status); err != nil {
		return status, fmt.Errorf("failed to save status: %w", err)
	}

	// Update deployment image
	deploymentName := request.Name
	if err := ff.deploymentMgr.UpdateImage(ctx, request.Namespace, deploymentName, request.NewImage); err != nil {
		ff.updateStatusWithError(ctx, status, "failed", "update_deployment", err)
		return status, err
	}

	if err := ff.kubeClient.WaitForDeploymentReady(ctx, request.Namespace, deploymentName, 5*time.Minute); err != nil {
		ff.updateStatusWithError(ctx, status, "failed", "wait_deployment_ready", err)
		return status, err
	}

	// Update ConfigMap if provided
	configMapName := status.Metadata["configmap_name"].(string)
	newFlags := status.Metadata["flags"].(map[string]string)
	rolloutPercent := status.Metadata["rollout_percent"].(int)
	if request.FeatureFlagConfig != nil {
		newFlags = request.FeatureFlagConfig.Flags
		rolloutPercent = request.FeatureFlagConfig.RolloutPercent
		configMapName = request.FeatureFlagConfig.ConfigMapName
		if err := ff.updateConfigMap(ctx, request.Namespace, configMapName, newFlags); err != nil {
			ff.updateStatusWithError(ctx, status, "failed", "update_configmap", err)
			return status, err
		}
	}

	// Perform health check if enabled
	if request.HealthCheckConfig != nil && request.HealthCheckConfig.Enabled {
		if err := ff.performHealthCheck(ctx, request.Namespace, deploymentName, request.HealthCheckConfig); err != nil {
			ff.updateStatusWithError(ctx, status, "failed", "health_check", err)
			return status, err
		}
	}

	// Monitor deployment for stability
	if err := ff.monitorDeployment(ctx, request, status); err != nil {
		ff.updateStatusWithError(ctx, status, "failed", "monitor", err)
		return status, err
	}

	status.Metadata["configmap_name"] = configMapName
	status.Metadata["flags"] = newFlags
	status.Metadata["rollout_percent"] = rolloutPercent
	status.Status = "success"
	status.CurrentPhase = "completed"
	status.EndTime = &[]time.Time{time.Now()}[0]
	ff.addEvent(status, "info", "completed", "Feature-flag deployment updated successfully")
	if err := ff.storage.SaveDeployment(ctx, status); err != nil {
		return status, fmt.Errorf("failed to save final status: %w", err)
	}
	return status, nil
}

func (ff *FeatureFlagStrategy) Rollback(ctx context.Context, request *types.RollbackRequest) error {
	status, err := ff.storage.GetDeploymentByName(ctx, request.Namespace, request.Name)
	if err != nil {
		return fmt.Errorf("failed to get deployment status: %w", err)
	}

	// Reset ConfigMap to empty flags or previous state
	configMapName := status.Metadata["configmap_name"].(string)
	if err := ff.configMapMgr.UpdateConfigMapData(ctx, request.Namespace, configMapName, map[string]string{}); err != nil {
		ff.logger.Warn("failed to reset configmap during rollback", zap.Error(err))
	}

	// Scale down deployment if needed
	if err := ff.kubeClient.ScaleDeployment(ctx, request.Namespace, request.Name, 0); err != nil {
		ff.logger.Warn("failed to scale down deployment during rollback", zap.Error(err))
	}

	status.Status = "rolled-back"
	status.EndTime = &[]time.Time{time.Now()}[0]
	ff.addEvent(status, "info", "rollback", fmt.Sprintf("Rolled back feature-flag deployment: %s", request.Reason))
	return ff.storage.SaveDeployment(ctx, status)
}

func (ff *FeatureFlagStrategy) GetStatus(ctx context.Context, namespace, name string) (*storage.DeploymentStatus, error) {
	return ff.storage.GetDeploymentByName(ctx, namespace, name)
}

func (ff *FeatureFlagStrategy) validateCreateRequest(request *types.DeploymentCreateRequest) error {
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
	if request.Strategy != "feature-flag" {
		return fmt.Errorf("strategy must be feature-flag")
	}
	if request.FeatureFlagConfig == nil {
		return fmt.Errorf("featureFlagConfig is required for feature-flag strategy")
	}
	if request.FeatureFlagConfig.ConfigMapName == "" {
		return fmt.Errorf("configMapName is required in featureFlagConfig")
	}
	if len(request.FeatureFlagConfig.Flags) == 0 {
		return fmt.Errorf("flags must not be empty in featureFlagConfig")
	}
	if request.FeatureFlagConfig.RolloutPercent < 0 || request.FeatureFlagConfig.RolloutPercent > 100 {
		return fmt.Errorf("rolloutPercent must be between 0 and 100")
	}
	return nil
}

func (ff *FeatureFlagStrategy) createDeployment(ctx context.Context, namespace, name, image string, replicas int32) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: image,
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: name,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if err := ff.kubeClient.CreateDeployment(ctx, deployment); err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}
	return ff.kubeClient.WaitForDeploymentReady(ctx, namespace, name, 5*time.Minute)
}

func (ff *FeatureFlagStrategy) createConfigMap(ctx context.Context, namespace, name string, flags map[string]string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: flags,
	}
	if err := ff.configMapMgr.CreateConfigMap(ctx, configMap); err != nil {
		return fmt.Errorf("failed to create configmap: %w", err)
	}
	return nil
}

func (ff *FeatureFlagStrategy) updateConfigMap(ctx context.Context, namespace, name string, flags map[string]string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: flags,
	}
	if err := ff.configMapMgr.UpdateConfigMap(ctx, configMap); err != nil {
		return fmt.Errorf("failed to update configmap: %w", err)
	}
	return nil
}

func (ff *FeatureFlagStrategy) performHealthCheck(ctx context.Context, namespace, deploymentName string, config *types.HealthCheckConfig) error {
	result, err := ff.healthMonitor.CheckDeploymentHealth(ctx, namespace, deploymentName, config)
	if err != nil || !result.Healthy {
		return fmt.Errorf("health check failed for %s: %w", deploymentName, err)
	}
	return nil
}

func (ff *FeatureFlagStrategy) monitorDeployment(ctx context.Context, request *types.DeploymentUpdateRequest, status *storage.DeploymentStatus) error {
	monitorDuration := 2 * time.Minute
	interval := 10 * time.Second
	start := time.Now()
	for {
		if request.HealthCheckConfig != nil && request.HealthCheckConfig.Enabled {
			if err := ff.performHealthCheck(ctx, request.Namespace, request.Name, request.HealthCheckConfig); err != nil {
				return fmt.Errorf("monitoring health check failed: %w", err)
			}
		}
		if time.Since(start) > monitorDuration {
			break
		}
		time.Sleep(interval)
	}
	ff.addEvent(status, "info", "monitoring", "Deployment remained healthy during monitoring")
	return nil
}

func (ff *FeatureFlagStrategy) addEvent(status *storage.DeploymentStatus, level, phase, message string) {
	status.Events = append(status.Events, storage.DeploymentEvent{
		Timestamp: time.Now(),
		Phase:     phase,
		Message:   message,
		Level:     level,
	})
	ff.logger.Info("event", zap.String("id", status.ID), zap.String("phase", phase), zap.String("message", message))
}

func (ff *FeatureFlagStrategy) updateStatusWithError(ctx context.Context, status *storage.DeploymentStatus, statusValue, phase string, err error) {
	status.Status = statusValue
	status.CurrentPhase = phase
	status.Error = err.Error()
	status.EndTime = &[]time.Time{time.Now()}[0]
	ff.addEvent(status, "error", phase, err.Error())
	ff.storage.SaveDeployment(ctx, status)
}