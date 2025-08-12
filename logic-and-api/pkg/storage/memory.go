// pkg/storage/memory.go
package storage

import (
	"strings"
	"sync"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type MemoryStorage struct {
    deployments map[string]*types.DeploymentStatus
    mutex       sync.RWMutex
}

func NewMemoryStorage() *MemoryStorage {
    return &MemoryStorage{
        deployments: make(map[string]*types.DeploymentStatus),
    }
}

func (ms *MemoryStorage) SaveDeployment(id string, status *types.DeploymentStatus) error {
    ms.mutex.Lock()
    defer ms.mutex.Unlock()
    
    // Create a copy to avoid race conditions
    statusCopy := *status
    statusCopy.Events = make([]types.DeploymentEvent, len(status.Events))
    copy(statusCopy.Events, status.Events)
    
    ms.deployments[id] = &statusCopy
    return nil
}

func (ms *MemoryStorage) GetDeployment(id string) (*types.DeploymentStatus, error) {
    ms.mutex.RLock()
    defer ms.mutex.RUnlock()
    
    status, exists := ms.deployments[id]
    if !exists {
        return nil, ErrNotFound
    }
    
    // Return a copy
    statusCopy := *status
    statusCopy.Events = make([]types.DeploymentEvent, len(status.Events))
    copy(statusCopy.Events, status.Events)
    
    return &statusCopy, nil
}

func (ms *MemoryStorage) ListDeployments(filters DeploymentFilters) ([]*types.DeploymentStatus, error) {
    ms.mutex.RLock()
    defer ms.mutex.RUnlock()
    
    var result []*types.DeploymentStatus
    
    for _, deployment := range ms.deployments {
        if ms.matchesFilters(deployment, filters) {
            // Create a copy
            deploymentCopy := *deployment
            deploymentCopy.Events = make([]types.DeploymentEvent, len(deployment.Events))
            copy(deploymentCopy.Events, deployment.Events)
            
            result = append(result, &deploymentCopy)
        }
    }
    
    return result, nil
}

func (ms *MemoryStorage) DeleteDeployment(id string) error {
    ms.mutex.Lock()
    defer ms.mutex.Unlock()
    
    if _, exists := ms.deployments[id]; !exists {
        return ErrNotFound
    }
    
    delete(ms.deployments, id)
    return nil
}

func (ms *MemoryStorage) matchesFilters(deployment *types.DeploymentStatus, filters DeploymentFilters) bool {
    if filters.Namespace != "" && !strings.EqualFold(deployment.Namespace, filters.Namespace) {
        return false
    }
    
    if filters.AppName != "" && !strings.EqualFold(deployment.Name, filters.AppName) {
        return false
    }
    
    if filters.Status != "" && !strings.EqualFold(deployment.Status, filters.Status) {
        return false
    }
    
    return true
}