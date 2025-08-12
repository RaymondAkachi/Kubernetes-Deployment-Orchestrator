package kubernetes

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConfigMapManager struct {
    client *Client
    logger *zap.Logger
}

func NewConfigMapManager(client *Client, logger *zap.Logger) *ConfigMapManager {
    return &ConfigMapManager{
        client: client,
        logger: logger,
    }
}

func (cm *ConfigMapManager) GetConfigMap(namespace, name string) (*corev1.ConfigMap, error) {
    configMap, err := cm.client.clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to get configmap %s/%s: %w", namespace, name, err)
    }
    return configMap, nil
}

func(cm *ConfigMapManager) CreateConfigMap(ctx context.Context, configMap *corev1.ConfigMap) (error) {
    _, err := cm.client.clientset.CoreV1().ConfigMaps(configMap.Namespace).Create(ctx, configMap, metav1.CreateOptions{})
    if err != nil {
        return fmt.Errorf("failed to create configmap: %w", err)
    }
    return  nil
}

func(cm *ConfigMapManager) UpdateConfigMap(ctx context.Context, configMap *corev1.ConfigMap) (error) {
    _, err := cm.client.clientset.CoreV1().ConfigMaps(configMap.Namespace).Update(ctx, configMap, metav1.UpdateOptions{})
    if err != nil {
        return fmt.Errorf("failed to update configmap: %w", err)
    }
    return nil
}

func(cm *ConfigMapManager) UpdateConfigMapData(ctx context.Context, namespace, name string, data map[string]string) (error) {
    configMap, err := cm.GetConfigMap(namespace, name)
    if err != nil {
        return fmt.Errorf("failed to get configmap: %w", err)
    }

    // Update the data
    if configMap.Data == nil {
        configMap.Data = make(map[string]string)
    }
    for key, value := range data {
        configMap.Data[key] = value
    }

    err = cm.UpdateConfigMap(ctx, configMap)
    if err != nil {
        return fmt.Errorf("failed to update configmap data: %w", err)
    }

    cm.logger.Info("configmap data updated",
        zap.String("namespace", namespace),
        zap.String("name", name))
    
    return nil
}
