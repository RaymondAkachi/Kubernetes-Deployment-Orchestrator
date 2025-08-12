// pkg/kubernetes/ingress.go
package kubernetes

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IngressManager struct {
    client *Client
    logger *zap.Logger
}

func NewIngressManager(client *Client, logger *zap.Logger) *IngressManager {
    return &IngressManager{
        client: client,
        logger: logger,
    }
}

func (im *IngressManager) GetIngress(ctx context.Context, namespace, name string) (*networkingv1.Ingress, error) {
    ingress, err := im.client.clientset.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to get ingress %s/%s: %w", namespace, name, err)
    }
    return ingress, nil
}

func (im *IngressManager) UpdateIngress(ctx context.Context, ingress *networkingv1.Ingress) (*networkingv1.Ingress, error) {
    updatedIngress, err := im.client.clientset.NetworkingV1().Ingresses(ingress.Namespace).Update(ctx, ingress, metav1.UpdateOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to update ingress: %w", err)
    }
    return updatedIngress, nil
}

func (im *IngressManager) SwitchTrafficTo(ctx context.Context, namespace, ingressName, targetService string) error {
    ingress, err := im.GetIngress(ctx, namespace, ingressName)
    if err != nil {
        return err
    }
    
    // Update ingress backend service
    for i, rule := range ingress.Spec.Rules {
        if rule.HTTP != nil {
            for j, path := range rule.HTTP.Paths {
                if path.Backend.Service != nil {
                    ingress.Spec.Rules[i].HTTP.Paths[j].Backend.Service.Name = targetService
                }
            }
        }
    }
    
    // Add tracking annotation
    if ingress.Annotations == nil {
        ingress.Annotations = make(map[string]string)
    }
    ingress.Annotations["orchestrator.io/target-service"] = targetService
    ingress.Annotations["orchestrator.io/switched-at"] = metav1.Now().Format(time.RFC3339)
    
    _, err = im.UpdateIngress(ctx, ingress)
    if err != nil {
        return fmt.Errorf("failed to switch ingress traffic: %w", err)
    }
    
    im.logger.Info("ingress traffic switched",
        zap.String("namespace", namespace),
        zap.String("ingress", ingressName),
        zap.String("target", targetService))
    
    return nil
}