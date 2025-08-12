package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
)

type Config struct {
	Namespace     string
	ConfigMapName string
	DeploymentName string
	AnnotationKey  string
}

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Load configuration
	cfg := Config{
		Namespace:     getEnv("NAMESPACE", "default"),
		ConfigMapName: getEnv("CONFIGMAP_NAME", "orchestrator-config"),
		DeploymentName: getEnv("DEPLOYMENT_NAME", "orchestrator"),
		AnnotationKey: getEnv("ANNOTATION_KEY", "config-checksum"),
	}
	logger.Info("Starting config watcher", zap.Any("config", cfg))

	// Initialize Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to initialize Kubernetes client", zap.Error(err))
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Fatal("Failed to create Kubernetes clientset", zap.Error(err))
	}

	// Initialize informer with ConfigMap filter
	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		0,
		informers.WithNamespace(cfg.Namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = "metadata.name=" + cfg.ConfigMapName
		}),
	)
	configMapInformer := factory.Core().V1().ConfigMaps().Informer()
	configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleConfigMapChange(obj, clientset, cfg, logger)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			handleConfigMapChange(newObj, clientset, cfg, logger)
		},
	})

	// Start informer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	factory.Start(ctx.Done())

	// Wait for informer cache to sync
	if !cache.WaitForCacheSync(ctx.Done(), configMapInformer.HasSynced) {
		logger.Fatal("Failed to sync informer cache")
	}

	// Handle graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	logger.Info("Shutting down config watcher")
	cancel()
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func calculateChecksum(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func handleConfigMapChange(obj interface{}, clientset *kubernetes.Clientset, cfg Config, logger *zap.Logger) {
	cm, ok := obj.(*v1.ConfigMap)
	if !ok || cm.Name != cfg.ConfigMapName {
		return
	}

	data, exists := cm.Data["config.yaml"]
	if !exists {
		logger.Warn("ConfigMap missing config.yaml", zap.String("configmap", cfg.ConfigMapName))
		return
	}

	checksum := calculateChecksum(data)
	logger.Info("Detected ConfigMap change", zap.String("configmap", cfg.ConfigMapName), zap.String("checksum", checksum))

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		dep, err := clientset.AppsV1().Deployments(cfg.Namespace).Get(ctx, cfg.DeploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}

		if dep.Spec.Template.Annotations[cfg.AnnotationKey] != checksum {
			dep.Spec.Template.Annotations[cfg.AnnotationKey] = checksum
			_, err = clientset.AppsV1().Deployments(cfg.Namespace).Update(ctx, dep, metav1.UpdateOptions{})
			return err
		}
		return nil
	})

	if err != nil {
		logger.Error("Failed to update deployment", zap.Error(err), zap.String("deployment", cfg.DeploymentName))
	} else {
		logger.Info("Triggered deployment restart", zap.String("deployment", cfg.DeploymentName), zap.String("checksum", checksum))
	}
}
