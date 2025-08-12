package monitoring

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/kubernetes"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type HealthResult struct {
	Healthy      bool          `json:"healthy"`
	Message      string        `json:"message"`
	CheckResults []CheckResult `json:"check_results"`
	Timestamp    time.Time     `json:"timestamp"`
}

type CheckResult struct {
	Name      string  `json:"name"`
	Passed    bool    `json:"passed"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Operator  string  `json:"operator"`
	Message   string  `json:"message"`
}

type HealthMonitor struct {
	prometheusClient *PrometheusClient
	kubeClient       *kubernetes.Client
	logger           *zap.Logger
	metrics          *Metrics
}

func NewHealthMonitor(prometheusClient *PrometheusClient, kubeClient *kubernetes.Client, logger *zap.Logger, metrics *Metrics) *HealthMonitor {
	return &HealthMonitor{
		prometheusClient: prometheusClient,
		kubeClient:       kubeClient,
		logger:           logger,
		metrics:          metrics,
	}
}

func (hm *HealthMonitor) CheckDeploymentHealth(ctx context.Context, namespace, deploymentName string, config *types.HealthCheckConfig) (*HealthResult, error) {
	start := time.Now()
	result := &HealthResult{
		Healthy:      true,
		CheckResults: []CheckResult{},
		Timestamp:    time.Now(),
	}

	// Check Kubernetes readiness
	if err := hm.checkKubernetesHealth(ctx, namespace, deploymentName, result); err != nil {
		hm.logger.Error("Kubernetes health check failed", zap.Error(err), zap.String("namespace", namespace), zap.String("deployment", deploymentName))
		result.Healthy = false
		result.Message = fmt.Sprintf("Kubernetes health check failed: %v", err)
		hm.metrics.RecordHealthCheckFailure(namespace, deploymentName, "kubernetes")
		return result, nil
	}

	// Check Prometheus metrics if configured
	if len(config.PrometheusQueries) > 0 {
		if err := hm.checkPrometheusMetrics(ctx, namespace, deploymentName, config.PrometheusQueries, result); err != nil {
			hm.logger.Error("Prometheus metrics check failed", zap.Error(err), zap.String("namespace", namespace), zap.String("deployment", deploymentName))
			result.Healthy = false
			result.Message = fmt.Sprintf("Prometheus metrics check failed: %v", err)
			hm.metrics.RecordHealthCheckFailure(namespace, deploymentName, "prometheus")
			return result, nil
		}
	}

	if result.Healthy {
		result.Message = "All health checks passed"
	}

	hm.metrics.RecordHealthCheckDuration(namespace, deploymentName, "total", time.Since(start).Seconds())
	return result, nil
}

func (hm *HealthMonitor) checkKubernetesHealth(ctx context.Context, namespace, deploymentName string, result *HealthResult) error {
	start := time.Now()
	defer func() {
		hm.metrics.RecordHealthCheckDuration(namespace, deploymentName, "kubernetes", time.Since(start).Seconds())
	}()

	deployment, err := hm.kubeClient.GetDeployment(ctx, namespace, deploymentName)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	if deployment.Status.ReadyReplicas == 0 {
		result.Healthy = false
		result.CheckResults = append(result.CheckResults, CheckResult{
			Name:    "kubernetes_ready_replicas",
			Passed:  false,
			Value:   float64(deployment.Status.ReadyReplicas),
			Message: "No ready replicas available",
		})
		return nil
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == "Available" && condition.Status != "True" {
			result.Healthy = false
			result.CheckResults = append(result.CheckResults, CheckResult{
				Name:    "kubernetes_available",
				Passed:  false,
				Message: fmt.Sprintf("Deployment not available: %s", condition.Message),
			})
			return nil
		}
	}

	result.CheckResults = append(result.CheckResults, CheckResult{
		Name:    "kubernetes_health",
		Passed:  true,
		Value:   float64(deployment.Status.ReadyReplicas),
		Message: "Kubernetes health check passed",
	})

	return nil
}

func (hm *HealthMonitor) checkPrometheusMetrics(ctx context.Context, namespace, deploymentName string, queries []types.PrometheusMetricQuery, result *HealthResult) error {
	start := time.Now()
	var wg sync.WaitGroup
	results := make(chan CheckResult, len(queries))
	errors := make(chan error, len(queries))

	for _, query := range queries {
		wg.Add(1)
		go func(q types.PrometheusMetricQuery) {
			defer wg.Done()
			actualQuery := hm.buildPrometheusQuery(q.Query, namespace, deploymentName)
			queryStart := time.Now()
			passed, value, err := hm.prometheusClient.EvaluateQuery(ctx, &types.PrometheusMetricQuery{
				Name:      q.Name,
				Query:     actualQuery,
				Threshold: q.Threshold,
				Operator:  q.Operator,
			})
			hm.metrics.RecordHealthCheckDuration(namespace, deploymentName, q.Name, time.Since(queryStart).Seconds())
			if err != nil {
				hm.logger.Error("Prometheus query failed", zap.String("query", actualQuery), zap.Error(err))
				errors <- err
				return
			}
			results <- CheckResult{
				Name:      q.Name,
				Passed:    passed,
				Value:     value,
				Threshold: q.Threshold,
				Operator:  q.Operator,
				Message:   hm.buildCheckMessage(passed, value, q.Threshold, q.Operator),
			}
		}(query)
	}

	wg.Wait()
	close(results)
	close(errors)

	for err := range errors {
		result.Healthy = false
		result.CheckResults = append(result.CheckResults, CheckResult{
			Name:    "prometheus_error",
			Passed:  false,
			Message: fmt.Sprintf("Query failed: %v", err),
		})
	}

	for res := range results {
		if !res.Passed {
			result.Healthy = false
			hm.metrics.RecordHealthCheckFailure(namespace, deploymentName, res.Name)
		}
		result.CheckResults = append(result.CheckResults, res)
	}

	hm.metrics.RecordHealthCheckDuration(namespace, deploymentName, "prometheus", time.Since(start).Seconds())
	return nil
}

func (hm *HealthMonitor) buildPrometheusQuery(queryTemplate, namespace, deploymentName string) string {
	query := queryTemplate
	query = strings.ReplaceAll(query, "{{namespace}}", namespace)
	query = strings.ReplaceAll(query, "{{deployment}}", deploymentName)
	return query
}

func (hm *HealthMonitor) buildCheckMessage(passed bool, value, threshold float64, operator string) string {
	status := "PASS"
	if !passed {
		status = "FAIL"
	}
	return fmt.Sprintf("%s: value=%.2f %s threshold=%.2f", status, value, operator, threshold)
}
