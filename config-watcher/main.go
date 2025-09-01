package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
)

type Config struct {
	Namespace      string
	ConfigMapName  string
	DeploymentName string
	AnnotationKey  string
}

type ConfigMapState struct {
	Version    string
	Data       string
	Checksum   string
	Timestamp  time.Time
}

type ConfigWatcher struct {
	clientset       *kubernetes.Clientset
	cfg            Config
	logger         *zap.Logger
	previousState  *ConfigMapState
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
		Namespace:      getEnv("NAMESPACE", "default"),
		ConfigMapName:  getEnv("CONFIGMAP_NAME", "orchestrator-config"),
		DeploymentName: getEnv("DEPLOYMENT_NAME", "orchestrator"),
		AnnotationKey:  getEnv("ANNOTATION_KEY", "config-checksum"),
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

	// Initialize config watcher
	watcher := &ConfigWatcher{
		clientset: clientset,
		cfg:       cfg,
		logger:    logger,
	}

	// Store initial state of ConfigMap
	if err := watcher.captureInitialState(); err != nil {
		logger.Fatal("Failed to capture initial ConfigMap state", zap.Error(err))
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
			watcher.handleConfigMapChange(obj, "ADD")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			watcher.handleConfigMapChange(newObj, "UPDATE")
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

	logger.Info("Config watcher started successfully")

	// Handle graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	logger.Info("Shutting down config watcher")
	cancel()
}

func (w *ConfigWatcher) captureInitialState() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cm, err := w.clientset.CoreV1().ConfigMaps(w.cfg.Namespace).Get(ctx, w.cfg.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get initial ConfigMap state: %w", err)
	}

	data, exists := cm.Data["config.yaml"]
	if !exists {
		return fmt.Errorf("ConfigMap missing config.yaml key")
	}

	w.previousState = &ConfigMapState{
		Version:   cm.ResourceVersion,
		Data:      data,
		Checksum:  calculateChecksum(data),
		Timestamp: time.Now(),
	}

	w.logger.Info("Captured initial ConfigMap state", 
		zap.String("version", w.previousState.Version),
		zap.String("checksum", w.previousState.Checksum))
	
	return nil
}

func (w *ConfigWatcher) validateConfig(configData string) error {
	// Create a temporary file to write config data for viper
	tmpFile, err := os.CreateTemp("", "config-validation-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write config data to temporary file
	if _, err := tmpFile.WriteString(configData); err != nil {
		return fmt.Errorf("failed to write config to temporary file: %w", err)
	}

	// Validate YAML syntax first
	var yamlTest interface{}
	if err := yaml.Unmarshal([]byte(configData), &yamlTest); err != nil {
		return fmt.Errorf("invalid YAML syntax: %w", err)
	}

	// Use viper to parse and validate against config struct
	v := viper.New()
	v.SetConfigFile(tmpFile.Name())
	
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var cfg YamlConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate the config struct using the built-in validator
	if err := validateConfigStruct(&cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}


func (w *ConfigWatcher) revertConfigMap(ctx context.Context) error {
	if w.previousState == nil {
		return fmt.Errorf("no previous state to revert to")
	}

	w.logger.Warn("Reverting ConfigMap to previous valid state", 
		zap.String("previous_checksum", w.previousState.Checksum))

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get current ConfigMap
		cm, err := w.clientset.CoreV1().ConfigMaps(w.cfg.Namespace).Get(ctx, w.cfg.ConfigMapName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Revert the data
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		cm.Data["config.yaml"] = w.previousState.Data

		// Add annotation about the revert
		if cm.Annotations == nil {
			cm.Annotations = make(map[string]string)
		}
		cm.Annotations["last-revert"] = time.Now().Format(time.RFC3339)
		cm.Annotations["revert-reason"] = "validation-failed"

		// Update ConfigMap
		_, err = w.clientset.CoreV1().ConfigMaps(w.cfg.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

func (w *ConfigWatcher) updateDeployment(ctx context.Context, checksum string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		dep, err := w.clientset.AppsV1().Deployments(w.cfg.Namespace).Get(ctx, w.cfg.DeploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = make(map[string]string)
		}

		// Update checksum annotation to trigger restart
		if dep.Spec.Template.Annotations[w.cfg.AnnotationKey] != checksum {
			dep.Spec.Template.Annotations[w.cfg.AnnotationKey] = checksum
			dep.Spec.Template.Annotations["config-updated"] = time.Now().Format(time.RFC3339)
			
			_, err = w.clientset.AppsV1().Deployments(w.cfg.Namespace).Update(ctx, dep, metav1.UpdateOptions{})
			return err
		}
		return nil
	})
}

func (w *ConfigWatcher) handleConfigMapChange(obj interface{}, eventType string) {
	cm, ok := obj.(*v1.ConfigMap)
	if !ok || cm.Name != w.cfg.ConfigMapName {
		return
	}

	data, exists := cm.Data["config.yaml"]
	if !exists {
		w.logger.Warn("ConfigMap missing config.yaml", zap.String("configmap", w.cfg.ConfigMapName))
		return
	}

	checksum := calculateChecksum(data)
	
	// Skip if this is the same as current state (avoid reprocessing reverts)
	if w.previousState != nil && w.previousState.Checksum == checksum {
		w.logger.Debug("Skipping processing - same checksum as previous state", zap.String("checksum", checksum))
		return
	}

	w.logger.Info("Detected ConfigMap change", 
		zap.String("configmap", w.cfg.ConfigMapName),
		zap.String("checksum", checksum),
		zap.String("event_type", eventType))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Validate the new configuration
	if err := w.validateConfig(data); err != nil {
		w.logger.Error("Configuration validation failed", 
			zap.Error(err),
			zap.String("checksum", checksum))

		// Revert to previous valid state
		if revertErr := w.revertConfigMap(ctx); revertErr != nil {
			w.logger.Error("Failed to revert ConfigMap", 
				zap.Error(revertErr),
				zap.String("original_error", err.Error()))
		} else {
			w.logger.Info("Successfully reverted ConfigMap to previous valid state")
		}
		return
	}

	// Configuration is valid, update deployment
	if err := w.updateDeployment(ctx, checksum); err != nil {
		w.logger.Error("Failed to update deployment", 
			zap.Error(err),
			zap.String("deployment", w.cfg.DeploymentName))
		return
	}

	// Update our state tracking
	w.previousState = &ConfigMapState{
		Version:   cm.ResourceVersion,
		Data:      data,
		Checksum:  checksum,
		Timestamp: time.Now(),
	}

	w.logger.Info("Successfully processed valid configuration change", 
		zap.String("deployment", w.cfg.DeploymentName),
		zap.String("checksum", checksum))
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