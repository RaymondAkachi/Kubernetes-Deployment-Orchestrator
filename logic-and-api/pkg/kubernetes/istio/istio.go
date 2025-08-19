// pkg/istio/istio.go
package istio

import (
	"context"
	"fmt"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"go.uber.org/zap"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"

	// Removed duplicate import for istioNetworkingV1alpha3
	// istioNetworkingV1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioClient "istio.io/client-go/pkg/clientset/versioned"

	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	// "sigs.k8s.io/controller-runtime/pkg/client"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
)

// IstioManager defines the interface for managing Istio resources
type IstioManager interface {
	CreateDestinationRule(ctx context.Context, namespace, serviceName string, subsets []Subset) error
	CreateVirtualService(ctx context.Context, namespace, serviceName string, hosts []string, routes []Route) error
	UpdateVirtualServiceWeights(ctx context.Context, namespace, serviceName string, weights map[string]int) error
	UpdateVirtualServiceRoutes(ctx context.Context, namespace, serviceName string, routes []Route) error
	DeleteVirtualService(ctx context.Context, namespace, serviceName string) error
	DeleteDestinationRule(ctx context.Context, namespace, serviceName string) error
}

// Subset represents a subset in a DestinationRule
type Subset struct {
	Name   string
	Labels map[string]string
}

// Destination represents a route destination in a VirtualService
type Destination struct {
	Host   string
	Subset string
}

//Created because of A/B deployment
type Match struct {
	Name string
	Exact string
}

// Route represents a route in a VirtualService
type Route struct {
	Destination Destination
	Weight      int32
	Match    []Match //Added becasue of A/B Deployment
}

// istioManagerImpl implements IstioManager
type istioManagerImpl struct {
	client *istioClient.Clientset
	logger *zap.Logger
	config config.IstioConfig
	crClient crClient.Client // Controller-runtime client for CRD operations
}

// NewIstioManager creates a new IstioManager instance
func NewIstioManager(istioCfg config.IstioConfig, logger *zap.Logger, crclient crClient.Client) (IstioManager, error) {
	if !istioCfg.Enabled {
		return nil, fmt.Errorf("istio is disabled in configuration")
	}

	var restConfig *rest.Config
	var err error

	if istioCfg.InCluster {
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
	} else {
		configPath := istioCfg.ConfigPath
		if configPath == "" {
			configPath = clientcmd.RecommendedHomeFile
		}
		restConfig, err = clientcmd.BuildConfigFromFlags("", configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from %s: %w", configPath, err)
		}
	}

	// Override API server and CA cert for Istio
	restConfig.Host = istioCfg.APIServer
	restConfig.CAFile = istioCfg.CACert

	iclient, err := istioClient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Istio clientset: %w", err)
	}

	return &istioManagerImpl{
		crClient: crclient,
		client: iclient,
		logger: logger,
		config: istioCfg,
	}, nil
}

// CreateDestinationRule creates a new DestinationRule with the specified subsets
func (m *istioManagerImpl) CreateDestinationRule(ctx context.Context, namespace, serviceName string, subsets []Subset) error {
	if !m.config.Enabled {
		return fmt.Errorf("istio is disabled")
	}

	dr := &istio.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: networkingv1alpha3.DestinationRule{
			Host:    serviceName,
			Subsets: toIstioSubsets(subsets),
		},
	}
	_, err := m.client.NetworkingV1alpha3().DestinationRules(namespace).Create(ctx, dr, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		m.logger.Error("failed to create DestinationRule",
			zap.String("namespace", namespace),
			zap.String("name", serviceName),
			zap.Error(err))
		return fmt.Errorf("failed to create DestinationRule %s/%s: %w", namespace, serviceName, err)
	}
	m.logger.Info("created or found DestinationRule",
		zap.String("namespace", namespace),
		zap.String("name", serviceName))
	return nil
}

// CreateVirtualService creates a new VirtualService with the specified routes
func (m *istioManagerImpl) CreateVirtualService(ctx context.Context, namespace, serviceName string, hosts []string, routes []Route) error {
	if !m.config.Enabled {
		return fmt.Errorf("istio is disabled")
	}

	vs := &istio.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: networkingv1alpha3.VirtualService{
			Hosts: hosts,
			Http: []*networkingv1alpha3.HTTPRoute{
				{
					Route: toIstioRoutes(routes),
				},
			},
		},
	}
	_, err := m.client.NetworkingV1alpha3().VirtualServices(namespace).Create(ctx, vs, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			m.logger.Info("VirtualService already exists",
				zap.String("namespace", namespace),
				zap.String("name", serviceName))
			return nil
		}
		m.logger.Error("failed to create VirtualService",
			zap.String("namespace", namespace),
			zap.String("name", serviceName),
			zap.Error(err))
		return fmt.Errorf("failed to create VirtualService %s/%s: %w", namespace, serviceName, err)
	}
	m.logger.Info("created VirtualService",
		zap.String("namespace", namespace),
		zap.String("name", serviceName))
	return nil
}

// UpdateVirtualServiceWeights updates the weights of an existing VirtualService
func (m *istioManagerImpl) UpdateVirtualServiceWeights(ctx context.Context, namespace, serviceName string, weights map[string]int) error {
	if !m.config.Enabled {
		return fmt.Errorf("istio is disabled")
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		vs, err := m.client.NetworkingV1alpha3().VirtualServices(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			m.logger.Error("failed to get VirtualService",
				zap.String("namespace", namespace),
				zap.String("name", serviceName),
				zap.Error(err))
			return fmt.Errorf("failed to get VirtualService %s/%s: %w", namespace, serviceName, err)
		}

		routes := make([]*networkingv1alpha3.HTTPRouteDestination, 0, len(weights))
		for subset, weight := range weights {
			routes = append(routes, &networkingv1alpha3.HTTPRouteDestination{
				Destination: &networkingv1alpha3.Destination{
					Host:   serviceName,
					Subset: subset,
				},
				Weight: int32(weight),
			})
		}

		if len(vs.Spec.Http) == 0 {
			vs.Spec.Http = []*networkingv1alpha3.HTTPRoute{{}}
		}
		vs.Spec.Http[0].Route = routes

		_, err = m.client.NetworkingV1alpha3().VirtualServices(namespace).Update(ctx, vs, metav1.UpdateOptions{})
		if err != nil {
			m.logger.Error("failed to update VirtualService weights",
				zap.String("namespace", namespace),
				zap.String("name", serviceName),
				zap.Error(err))
			return fmt.Errorf("failed to update VirtualService %s/%s weights: %w", namespace, serviceName, err)
		}
		m.logger.Info("updated VirtualService weights",
			zap.String("namespace", namespace),
			zap.String("name", serviceName),
			zap.Any("weights", weights))
		return nil
	})
}


func (im *istioManagerImpl) UpdateVirtualServiceRoutes(ctx context.Context, namespace, serviceName string, routes []Route) error {
	// Fetch the existing VirtualService
	vs := &istio.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
	}
	if err := im.crClient.Get(ctx, crClient.ObjectKey{Namespace: namespace, Name: serviceName}, vs); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("VirtualService %s/%s not found: %w", namespace, serviceName, err)
		}
		return fmt.Errorf("failed to get VirtualService %s/%s: %w", namespace, serviceName, err)
	}

	// Convert routes to Istio HTTPRoute format
	httpRoutes := make([]*networkingv1alpha3.HTTPRoute, len(routes))
	for i, route := range routes {
		httpRoute := &networkingv1alpha3.HTTPRoute{
			Route: []*networkingv1alpha3.HTTPRouteDestination{
				{
					Destination: &networkingv1alpha3.Destination{
						Host:   route.Destination.Host,
						Subset: route.Destination.Subset,
					},
					Weight: int32(route.Weight),
				},
			},
		}
		if len(route.Match) > 0 {
			httpRoute.Match = make([]*networkingv1alpha3.HTTPMatchRequest, len(route.Match))
			for j, match := range route.Match {
				httpRoute.Match[j] = &networkingv1alpha3.HTTPMatchRequest{
					Headers: map[string]*networkingv1alpha3.StringMatch{
						match.Name: {MatchType: &networkingv1alpha3.StringMatch_Exact{Exact: match.Exact}},
					},
				}
			}
		}
		httpRoutes[i] = httpRoute
	}

	// Update the VirtualService spec
	vs.Spec.Http = httpRoutes

	// Apply the updated VirtualService
	if err := im.crClient.Update(ctx, vs); err != nil {
		im.logger.Error("failed to update VirtualService",
			zap.String("namespace", namespace),
			zap.String("name", serviceName),
			zap.Error(err))
		return fmt.Errorf("failed to update VirtualService %s/%s: %w", namespace, serviceName, err)
	}

	im.logger.Info("successfully updated VirtualService routes",
		zap.String("namespace", namespace),
		zap.String("name", serviceName),
		zap.Int("routes", len(httpRoutes)))
	return nil
}

// toIstioSubsets converts internal Subset type to Istio's Subset type
func toIstioSubsets(subsets []Subset) []*networkingv1alpha3.Subset {
	result := make([]*networkingv1alpha3.Subset, len(subsets))
	for i, s := range subsets {
		result[i] = &networkingv1alpha3.Subset{Name: s.Name, Labels: s.Labels}
	}
	return result
}

// toIstioRoutes converts internal Route type to Istio's HTTPRouteDestination type
func toIstioRoutes(routes []Route) []*networkingv1alpha3.HTTPRouteDestination {
	result := make([]*networkingv1alpha3.HTTPRouteDestination, len(routes))
	for i, r := range routes {
		result[i] = &networkingv1alpha3.HTTPRouteDestination{
			Destination: &networkingv1alpha3.Destination{
				Host:   r.Destination.Host,
				Subset: r.Destination.Subset,
			},
			Weight: int32(r.Weight),
		}
	}
	return result
}

func(im *istioManagerImpl) DeleteVirtualService(ctx context.Context, namespace, serviceName string) error {
	if !im.config.Enabled {
		return fmt.Errorf("istio is disabled")
	}

	err := im.client.NetworkingV1alpha3().VirtualServices(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			im.logger.Info("VirtualService not found for deletion",
				zap.String("namespace", namespace),
				zap.String("name", serviceName))
			return nil
		}
		im.logger.Error("failed to delete VirtualService",
			zap.String("namespace", namespace),
			zap.String("name", serviceName),
			zap.Error(err))
		return fmt.Errorf("failed to delete VirtualService %s/%s: %w", namespace, serviceName, err)
	}
	im.logger.Info("deleted VirtualService",
		zap.String("namespace", namespace),
		zap.String("name", serviceName))
	return nil
}

func(im *istioManagerImpl) DeleteDestinationRule(ctx context.Context, namespace, serviceName string) error {
	if !im.config.Enabled {
		return fmt.Errorf("istio is disabled")
	}

	err := im.client.NetworkingV1alpha3().DestinationRules(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			im.logger.Info("DestinationRule not found for deletion",
				zap.String("namespace", namespace),
				zap.String("name", serviceName))
			return nil
		}
		im.logger.Error("failed to delete DestinationRule",
			zap.String("namespace", namespace),
			zap.String("name", serviceName),
			zap.Error(err))
		return fmt.Errorf("failed to delete DestinationRule %s/%s: %w", namespace, serviceName, err)
	}
	im.logger.Info("deleted DestinationRule",
		zap.String("namespace", namespace),
		zap.String("name", serviceName))
	return nil
}