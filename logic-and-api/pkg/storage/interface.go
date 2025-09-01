// pkg/storage/interface.go
package storage

import (
	"context"
	"errors"

    "github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

var ErrNotFound = errors.New("deployment not found")

type DeploymentFilters struct {
    Namespace string
    AppName   string
    Status    string
}

// type Interface interface {
//     SaveDeployment(ctx context.Context, dep *DeploymentStatus) error
//     GetDeployment(ctx context.Context, id string) (*DeploymentStatus, error)
//     DeleteDeployment(ctx context.Context, id string) (error)
//     ListDeployments(ctx context.Context, namespace string) ([]*DeploymentStatus, error)
//     GetDeploymentByName(ctx context.Context, namespace, name string) (*DeploymentStatus, error)

// }

type Interface interface {
    Close(ctx context.Context) error
    CheckDeploymentExists(ctx context.Context, appName, namespace string) (bool, error)
    CreateDeployment(ctx context.Context, dep *DeploymentStatus) error
    UpdateDeployment(ctx context.Context, dep *DeploymentStatus) error
    GetDeployment(ctx context.Context, id string) (*DeploymentStatus, error)
    GetDeploymentByAppNamespace(ctx context.Context, appName, namespace string) (*DeploymentStatus, error) 
    DeleteDeployment(ctx context.Context, id string) error
    ListDeployments(ctx context.Context, namespace string) ([]*DeploymentStatus, error)
    ListDeploymentsFiltered(ctx context.Context, req *types.ListDeploymentsRequest) ([]*DeploymentStatus, error) 
}
