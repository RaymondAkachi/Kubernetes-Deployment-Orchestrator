// pkg/kubernetes/client.go
package kubernetes

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	// networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	// "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
)

type Client struct {
    clientset      *kubernetes.Clientset
    config         *rest.Config
    logger         *zap.Logger
    defaultTimeout time.Duration
    retryCount     int
    mu             sync.RWMutex
}

type ClientInterface interface {
    GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error)
    UpdateDeployment(ctx context.Context, deployment *appsv1.Deployment) (*appsv1.Deployment, error)
    ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error
    WaitForDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) error
    GetConfig() *rest.Config
}

func NewClient(cfg *config.Config, logger *zap.Logger) (*Client, error) {
    var k8sConfig *rest.Config
    var err error

    if cfg.Kubernetes.InCluster {
        k8sConfig, err = rest.InClusterConfig()
        if err != nil {
            return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
        }
    } else {
        configPath := os.Getenv("CONFIG_PATH")
        if configPath == "" {
            configPath = clientcmd.RecommendedHomeFile
        }
        k8sConfig, err = clientcmd.BuildConfigFromFlags("", configPath)
        if err != nil {
            return nil, fmt.Errorf("failed to build config from flags: %w", err)
        }
    }

    // Configure rate limiting
    k8sConfig.QPS = cfg.Kubernetes.RateLimit.QPS
    k8sConfig.Burst = cfg.Kubernetes.RateLimit.Burst

    clientset, err := kubernetes.NewForConfig(k8sConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to create clientset: %w", err)
    }

    return &Client{
        clientset:      clientset,
        config:         k8sConfig,
        logger:         logger,
        defaultTimeout: cfg.Kubernetes.Timeout,
        retryCount:     cfg.Kubernetes.RetryCount,
    }, nil
}

func (c *Client) CreateDeployment(ctx context.Context, deployment *appsv1.Deployment) ( error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    _, err := c.clientset.AppsV1().Deployments(deployment.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
    if err != nil {
        c.logger.Error("failed to create deployment",
            zap.String("namespace", deployment.Namespace),
            zap.String("name", deployment.Name),
            zap.Error(err))
        return fmt.Errorf("failed to create deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
    }

    return nil
}
func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    var deployment *appsv1.Deployment
    var err error
    
    retryErr := retry.OnError(
        retry.DefaultRetry,
        func(err error) bool {
            return !errors.IsNotFound(err)
        },
        func() error {
            deployment, err = c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
            return err
        },
    )
    
    if retryErr != nil {
        c.logger.Error("failed to get deployment",
            zap.String("namespace", namespace),
            zap.String("name", name),
            zap.Error(retryErr))
        return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, retryErr)
    }
    
    return deployment, nil
}

func (c *Client) UpdateDeployment(ctx context.Context, deployment *appsv1.Deployment) (*appsv1.Deployment, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    var updatedDeployment *appsv1.Deployment
    var err error
    
    retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
        updatedDeployment, err = c.clientset.AppsV1().Deployments(deployment.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
        return err
    })
    
    if retryErr != nil {
        c.logger.Error("failed to update deployment",
            zap.String("namespace", deployment.Namespace),
            zap.String("name", deployment.Name),
            zap.Error(retryErr))
        return nil, fmt.Errorf("failed to update deployment %s/%s: %w", deployment.Namespace, deployment.Name, retryErr)
    }
    
    return updatedDeployment, nil
}

func (c *Client) WaitForDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wait.PollImmediate(5*time.Second, timeout, func() (bool, error) {
		deployment, err := c.GetDeployment(ctx, namespace, name)
		if err != nil {
			// Handle cases where the deployment might not exist yet.
			// The PollImmediate will continue polling unless the error is fatal.
			if errors.IsNotFound(err) {
				return false, nil
			}
            
			return false, err
		}

		// Check for the "Available" condition.
		isAvailable := false
		isProgressing := false
		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
				isAvailable = true
			}
			if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionTrue {
				isProgressing = true
			}
		}

		// Check if the number of ready replicas matches the desired replicas.
		replicasReady := deployment.Status.ReadyReplicas == *deployment.Spec.Replicas

		// The deployment is ready only when all three conditions are met.
		if isAvailable && isProgressing && replicasReady {
			return true, nil
		}
		
		return false, nil
	})
}

func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
    scale, err := c.clientset.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
    if err != nil {
        return fmt.Errorf("failed to get deployment scale: %w", err)
    }
    
    scale.Spec.Replicas = replicas
    
    _, err = c.clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
    if err != nil {
        return fmt.Errorf("failed to scale deployment: %w", err)
    }
    
    return nil
}

func (c *Client) UpdateDepWithNamespace(ctx context.Context, namespace, deploymentname, newImage string) error {
    deployment, err := c.GetDeployment(ctx, namespace, deploymentname)
    if err != nil {
        return fmt.Errorf("failed to get deployment %v", deploymentname)
    }

    for i, container := range deployment.Spec.Template.Spec.Containers {
        // Assuming the main container is the first one or has specific name
        if i == 0 || strings.Contains(container.Name, "app") {
            deployment.Spec.Template.Spec.Containers[i].Image = newImage
            break
        }
    }
    return nil
}

func(c *Client) DeleteDeployment(ctx context.Context, namespace, name string) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    err := c.clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
    if err != nil {
        if errors.IsNotFound(err) {
            return fmt.Errorf("deployment %s/%s not found", namespace, name)
        }
        return fmt.Errorf("failed to delete deployment %s/%s: %w", namespace, name, err)
    }

    return nil
}

func(c *Client) GetConfig() *rest.Config{
    return c.config
}

func(c *Client) GetClientSet() *kubernetes.Clientset {
    return c.clientset
}