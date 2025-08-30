package monitoring

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/types"
)

type PrometheusClient struct {
	client api.Client
	api    v1.API
	logger *zap.Logger
	cache  *cache.Cache
}

func NewPrometheusClient(cfg *config.PrometheusConfig, logger *zap.Logger) (*PrometheusClient, error) {
	client, err := api.NewClient(api.Config{
		Address: cfg.URL,
		// Enable TLS for secure communication
		RoundTripper: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Allow non-tls for now
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus client: %w", err)
	}

	return &PrometheusClient{
		client: client,
		api:    v1.NewAPI(client),
		logger: logger,
		cache:  cache.New(5*time.Minute, 10*time.Minute), // Cache query results for 5 minutes
	}, nil
}

func (pc *PrometheusClient) Query(ctx context.Context, query string) (model.Value, error) {
	// Check cache first
	if cached, found := pc.cache.Get(query); found {
		pc.logger.Debug("Returning cached Prometheus query result", zap.String("query", query))
		return cached.(model.Value), nil
	}

	// Set query timeout
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, warnings, err := pc.api.Query(queryCtx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("prometheus query failed: %w", err)
	}

	if len(warnings) > 0 {
		pc.logger.Warn("Prometheus query warnings", zap.String("query", query), zap.Strings("warnings", warnings))
	}

	// Cache the result
	pc.cache.Set(query, result, cache.DefaultExpiration)
	return result, nil
}

func (pc *PrometheusClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	cacheKey := fmt.Sprintf("%s:%v:%v:%v", query, r.Start, r.End, r.Step)
	if cached, found := pc.cache.Get(cacheKey); found {
		pc.logger.Debug("Returning cached Prometheus range query result", zap.String("query", query))
		return cached.(model.Value), nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, warnings, err := pc.api.QueryRange(queryCtx, query, r)
	if err != nil {
		return nil, fmt.Errorf("prometheus range query failed: %w", err)
	}

	if len(warnings) > 0 {
		pc.logger.Warn("Prometheus range query warnings", zap.String("query", query), zap.Strings("warnings", warnings))
	}

	pc.cache.Set(cacheKey, result, cache.DefaultExpiration)
	return result, nil
}

func (pc *PrometheusClient) EvaluateQuery(ctx context.Context, prometheusQuery *types.PrometheusMetricQuery) (bool, float64, error) {
	result, err := pc.Query(ctx, prometheusQuery.Query)
	if err != nil {
		return false, 0, err
	}

	var value float64
	switch result.Type() {
	case model.ValVector:
		vector := result.(model.Vector)
		if len(vector) == 0 {
			return false, 0, fmt.Errorf("no data returned for query: %s", prometheusQuery.Query)
		}
		value = float64(vector[0].Value)
	case model.ValScalar:
		scalar := result.(*model.Scalar)
		value = float64(scalar.Value)
	default:
		return false, 0, fmt.Errorf("unsupported result type: %s", result.Type())
	}

	// Evaluate threshold
	var passed bool
	switch prometheusQuery.Operator {
	case "gt":
		passed = value > prometheusQuery.Threshold
	case "lt":
		passed = value < prometheusQuery.Threshold
	case "eq":
		passed = value == prometheusQuery.Threshold
	case "gte":
		passed = value >= prometheusQuery.Threshold
	case "lte":
		passed = value <= prometheusQuery.Threshold
	default:
		return false, 0, fmt.Errorf("unsupported operator: %s", prometheusQuery.Operator)
	}

	return passed, value, nil
}
