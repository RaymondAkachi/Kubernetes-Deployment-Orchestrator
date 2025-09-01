package kubernetes

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// LeaderElectionManager manages leader election operations
type LeaderElectionManager struct {
	elector *leaderelection.LeaderElector
	logger  *zap.Logger
}

// NewLeaderElectionManager creates a new LeaderElectionManager
func NewLeaderElectionManager(config *rest.Config, namespace, name string, logger *zap.Logger) (*LeaderElectionManager, error) {
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	// Use hostname as the identity for leader election
	identity, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	// Configure leader election
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Client: clientSet.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: identity,
		},
	}

	leaderElectionConfig := leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				logger.Info("became leader", zap.String("identity", identity))
			},
			OnStoppedLeading: func() {
				logger.Info("stopped leading", zap.String("identity", identity))
			},
			OnNewLeader: func(newLeaderIdentity string) {
				if newLeaderIdentity == identity {
					return
				}
				logger.Info("new leader elected", zap.String("new_leader", newLeaderIdentity))
			},
		},
	}

	elector, err := leaderelection.NewLeaderElector(leaderElectionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create leader elector: %w", err)
	}

	return &LeaderElectionManager{
		elector: elector,
		logger:  logger,
	}, nil
}

// Start runs the leader election process
func (lem *LeaderElectionManager) Start(ctx context.Context) {
	lem.logger.Info("starting leader election")
	go lem.elector.Run(ctx)
}

// IsLeader checks if the current instance is the leader
func (lem *LeaderElectionManager) IsLeader() bool {
	return lem.elector.IsLeader()
}

// Stop gracefully stops the leader election
func (lem *LeaderElectionManager) Stop() {
	lem.logger.Info("stopping leader election")
	lem.elector = nil
}
