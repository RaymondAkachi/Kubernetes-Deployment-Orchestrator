package strategies

import (
	"context"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/storage"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type DeploymentStrategy interface {
	CreateAppDeployment(ctx context.Context, request *types.DeploymentCreateRequest) (*storage.DeploymentStatus, error)
	UpdateAppDeployment(ctx context.Context, request *types.DeploymentUpdateRequest) (*storage.DeploymentStatus, error)
	Rollback(ctx context.Context, namespace, deploymentName string) (*storage.DeploymentStatus, error)
}