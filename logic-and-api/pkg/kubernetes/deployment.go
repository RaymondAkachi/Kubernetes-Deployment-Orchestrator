// pkg/kubernetes/deployment.go
package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentManager struct {
    client *Client
    logger *zap.Logger
}

func NewDeploymentManager(client *Client, logger *zap.Logger) *DeploymentManager {
    return &DeploymentManager{
        client: client,
        logger: logger,
    }
}

func (dm *DeploymentManager) UpdateDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
    _, err := dm.client.UpdateDeployment(ctx, deployment)
    if err != nil {
        return fmt.Errorf("failed to update deployment: %w", err)
    }
    dm.logger.Info("deployment updated",
        zap.String("namespace", deployment.Namespace),
        zap.String("name", deployment.Name))
    return nil
}

func (dm *DeploymentManager) UpdateImage(ctx context.Context, namespace, name, newImage string) error {
    deployment, err := dm.client.GetDeployment(ctx, namespace, name)
    if err != nil {
        return fmt.Errorf("failed to get deployment: %w", err)
    }
    
    // Update container image
    for i, container := range deployment.Spec.Template.Spec.Containers {
        // Assuming the main container is the first one or has specific name
        if i == 0 || strings.Contains(container.Name, "app") {
            deployment.Spec.Template.Spec.Containers[i].Image = newImage
            break
        }
    }
    
    // Add deployment annotations for tracking
    if deployment.Spec.Template.Annotations == nil {
        deployment.Spec.Template.Annotations = make(map[string]string)
    }
    deployment.Spec.Template.Annotations["orchestrator.io/updated-at"] = metav1.Now().Format(time.RFC3339)
    deployment.Spec.Template.Annotations["orchestrator.io/image"] = newImage
    
    _, err = dm.client.UpdateDeployment(ctx, deployment)
    if err != nil {
        return fmt.Errorf("failed to update deployment: %w", err)
    }
    
    dm.logger.Info("deployment image updated",
        zap.String("namespace", namespace),
        zap.String("name", name),
        zap.String("image", newImage))
    
    return nil
}

func (dm *DeploymentManager) GetReadyReplicas(ctx context.Context, namespace, name string) (int32, error) {
    deployment, err := dm.client.GetDeployment(ctx, namespace, name)
    if err != nil {
        return 0, err
    }
    
    return deployment.Status.ReadyReplicas, nil
}

func (dm *DeploymentManager) IsHealthy(ctx context.Context, namespace, name string) (bool, error) {
    deployment, err := dm.client.GetDeployment(ctx, namespace, name)
    if err != nil {
        return false, err
    }
    
    // Check if deployment has minimum required replicas ready
    if deployment.Status.ReadyReplicas < 1 {
        return false, nil
    }
    
    // Check deployment conditions
    for _, condition := range deployment.Status.Conditions {
        if condition.Type == appsv1.DeploymentProgressing {
            if condition.Status != corev1.ConditionTrue {
                return false, nil
            }
        }
        if condition.Type == appsv1.DeploymentAvailable {
            if condition.Status != corev1.ConditionTrue {
                return false, nil
            }
        }
    }
    
    return true, nil
}


func(m *DeploymentManager) DeleteDeployment(ctx context.Context, namespace, name string) error {
    err := m.client.DeleteDeployment(ctx, namespace, name)
    if err != nil {
        return fmt.Errorf("failed to delete deployment: %w", err)
    }
    
    m.logger.Info("deployment deleted",
        zap.String("namespace", namespace),
        zap.String("name", name))
    
    return nil
}

func(m *DeploymentManager) IsDeploymentReady(ctx context.Context, namespace, name string) (bool, error) {
    deployment, err := m.client.GetDeployment(ctx, namespace, name)
    if err != nil {
        return false, fmt.Errorf("failed to get deployment: %w", err)
    }
    
    // Check if the deployment is ready
    if deployment.Status.ReadyReplicas >= deployment.Status.Replicas {
        return true, nil
    }
    
    return false, nil
}   