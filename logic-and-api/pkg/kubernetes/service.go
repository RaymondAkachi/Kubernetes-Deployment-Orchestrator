// pkg/kubernetes/service.go
package kubernetes

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
)

type ServiceManager struct {
    client *Client
    logger *zap.Logger
}

func NewServiceManager(client *Client, logger *zap.Logger) *ServiceManager {
    return &ServiceManager{
        client: client,
        logger: logger,
    }
}

func (sm *ServiceManager) GetService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
    service, err := sm.client.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
    if err != nil{
        return nil, fmt.Errorf("failed to get service %s/%s: %w", namespace, name, err)
    }
    return service, nil
}

func (sm *ServiceManager) CreateService(ctx context.Context, service *corev1.Service) (*corev1.Service, error) {
    createdService, err := sm.client.clientset.CoreV1().Services(service.Namespace).Create(ctx, service, metav1.CreateOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to create service: %w", err)
    }
    return createdService, nil
}

func (sm *ServiceManager) UpdateService(ctx context.Context, service *corev1.Service) (*corev1.Service, error) {
    updatedService, err := sm.client.clientset.CoreV1().Services(service.Namespace).Update(ctx, service, metav1.UpdateOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to update service: %w", err)
    }
    return updatedService, nil
}

func (sm *ServiceManager) SwitchTrafficTo(ctx context.Context, namespace, serviceName, targetDeployment string) error {
    service, err := sm.GetService(ctx, namespace, serviceName)
    if err != nil {
        return err
    }
    
    // Get target deployment to extract its labels
    deployment, err := sm.client.GetDeployment(ctx, namespace, targetDeployment)
    if err != nil {
        return fmt.Errorf("failed to get target deployment: %w", err)
    }
    
    // Update service selector to match target deployment labels
    if service.Spec.Selector == nil {
        service.Spec.Selector = make(map[string]string)
    }
    
    // Copy deployment labels to service selector
    for key, value := range deployment.Spec.Template.Labels {
        // Only copy app-related labels, skip system labels
        if key == "app" || key == "version" || key == "environment" {
            service.Spec.Selector[key] = value
        }
    }
    
    // Add tracking annotation
    if service.Annotations == nil {
        service.Annotations = make(map[string]string)
    }
    service.Annotations["orchestrator.io/target-deployment"] = targetDeployment
    service.Annotations["orchestrator.io/switched-at"] = metav1.Now().Format(time.RFC3339)
    
    _, err = sm.UpdateService(ctx, service)
    if err != nil {
        return fmt.Errorf("failed to switch service traffic: %w", err)
    }
    
    sm.logger.Info("service traffic switched",
        zap.String("namespace", namespace),
        zap.String("service", serviceName),
        zap.String("target", targetDeployment))
    
    return nil
}

func (sm *ServiceManager) DeleteService(ctx context.Context, namespace, name string) error {
    err := sm.client.clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
    if err != nil {
        return fmt.Errorf("failed to delete service %s/%s: %w", namespace, name, err)
    }
    sm.logger.Info("service deleted",
        zap.String("namespace", namespace),
        zap.String("service", name))
    return nil
}

func (sm *ServiceManager) SetupService(ctx context.Context, namespace, name string, labels map[string]string) (*corev1.Service, error) {
    service := &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      name,
            Namespace: namespace,
            Labels:    labels,
        },
        Spec: corev1.ServiceSpec{
            Selector: labels,
            Ports: []corev1.ServicePort{
                {
                    Port: 80,
                    TargetPort: intstr.IntOrString{
                        IntVal: 80,
                    },
                    Protocol: corev1.ProtocolTCP,
                },
            },
            Type: corev1.ServiceTypeClusterIP,
        },
    }
    
    createdService, err := sm.CreateService(ctx, service)
    if err != nil {
        return nil, fmt.Errorf("failed to setup service: %w", err)
    }
    return createdService, nil
}


func(sm *ServiceManager) UpdateServiceSelector(ctx context.Context, namespace, name string, labels map[string]string) (error) {
    service, err := sm.GetService(ctx, namespace, name)
    if err != nil {
        return fmt.Errorf("failed to get service %s/%s: %w", namespace, name, err)
    }
    
    // Update the selector
    if service.Spec.Selector == nil {
        service.Spec.Selector = make(map[string]string)
    }
    
    for key, value := range labels {
        service.Spec.Selector[key] = value
    }
    
    _, err = sm.UpdateService(ctx, service)
    if err != nil {
        return fmt.Errorf("failed to update service selector: %w", err)
    }
    
    return err 
}