package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// The DeploymentStatus and other type definitions should be in a separate file (or at the top of this one)

// MongoDBClient wraps the MongoDB client and collection
type MongoDBClient struct {
    client     *mongo.Client
    collection *mongo.Collection
    logger     *zap.Logger
}

// NewMongoDBClient initializes a MongoDB client
func NewMongoDBClient(cfg *config.Config, logger *zap.Logger) (*MongoDBClient, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.Storage.MongoDB.URI))
    if err != nil {
        return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
    }

    collection := client.Database(cfg.Storage.MongoDB.Database).Collection(cfg.Storage.MongoDB.Collection)
    return &MongoDBClient{
        client:     client,
        collection: collection,
        logger:     logger,
    }, nil
}

// Close closes the MongoDB client
func (m *MongoDBClient) Close(ctx context.Context) error {
    return m.client.Disconnect(ctx)
}

// CheckDeploymentExists checks if a deployment with the given name and namespace exists.
func (m *MongoDBClient) CheckDeploymentExists(ctx context.Context, appName, namespace string) (bool, error) {
    filter := bson.M{
        "app_name":  appName,
        "namespace": namespace,
    }
    
    var dep DeploymentStatus
    err := m.collection.FindOne(ctx, filter).Decode(&dep)
    
    if err == mongo.ErrNoDocuments {
        return false, nil
    }
    if err != nil {
        m.logger.Error("Failed to check for existing deployment", zap.Error(err))
        return false, fmt.Errorf("database query error: %w", err)
    }
    
    return true, nil
}

// CreateDeployment creates a new deployment after checking for duplicates.
func (m *MongoDBClient) CreateDeployment(ctx context.Context, dep *DeploymentStatus) error {
    exists, err := m.CheckDeploymentExists(ctx, dep.AppName, dep.Namespace)
    if err != nil {
        m.logger.Error("Failed to check for existing deployment", zap.Error(err))
        return fmt.Errorf("failed to check for existing deployment: %w", err)
    }
    if exists {
        return fmt.Errorf("a deployment with the name '%s' already exists in namespace '%s'", dep.AppName, dep.Namespace)
    }

    _, err = m.collection.InsertOne(ctx, dep)
    if err != nil {
        m.logger.Error("Failed to save new deployment", zap.Error(err))
        return fmt.Errorf("failed to save new deployment: %w", err)
    }
    
    m.logger.Info("Successfully saved new deployment", zap.String("app_name", dep.AppName), zap.String("namespace", dep.Namespace))
    return nil
}

// UpdateDeployment updates an existing deployment record in the database.
func (m *MongoDBClient) UpdateDeployment(ctx context.Context, dep *DeploymentStatus) error {
    filter := bson.M{"_id": dep.ID}
    update := bson.M{
        "$set": dep,
    }

    result, err := m.collection.UpdateOne(ctx, filter, update)
    if err != nil {
        m.logger.Error("Failed to update deployment", zap.Error(err), zap.String("id", dep.ID))
        return fmt.Errorf("failed to update deployment: %w", err)
    }
    
    if result.MatchedCount == 0 {
        return fmt.Errorf("no deployment found with ID '%s' to update", dep.ID)
    }

    m.logger.Info("Successfully updated deployment", zap.String("id", dep.ID), zap.Int64("modified", result.ModifiedCount))
    return nil
}

// GetDeployment retrieves a deployment by its unique ID.
func (m *MongoDBClient) GetDeployment(ctx context.Context, id string) (*DeploymentStatus, error) {
    var dep DeploymentStatus
    filter := bson.M{"_id": id}
    err := m.collection.FindOne(ctx, filter).Decode(&dep)
    if err == mongo.ErrNoDocuments {
        return nil, nil
    }
    if err != nil {
        m.logger.Error("Failed to get deployment", zap.Error(err), zap.String("id", id))
        return nil, fmt.Errorf("failed to get deployment: %w", err)
    }
    return &dep, nil
}

// GetDeploymentByAppNamespace retrieves a deployment by its name and namespace.
func (m *MongoDBClient) GetDeploymentByAppNamespace(ctx context.Context, appName, namespace string) (*DeploymentStatus, error) {
    filter := bson.M{"app_name": appName, "namespace": namespace}
    
    var dep DeploymentStatus
    err := m.collection.FindOne(ctx, filter).Decode(&dep)
    
    if err == mongo.ErrNoDocuments {
        return nil, fmt.Errorf("no deployment found with name '%s' in namespace '%s'", appName, namespace)
    }
    if err != nil {
        m.logger.Error("Failed to get deployment by app name and namespace", zap.Error(err))
        return nil, fmt.Errorf("database query error: %w", err)
    }
    
    return &dep, nil
}

// DeleteDeployment deletes a deployment by ID
func (m *MongoDBClient) DeleteDeployment(ctx context.Context, id string) error {
    filter := bson.M{"_id": id}
    result, err := m.collection.DeleteOne(ctx, filter)
    if err != nil {
        m.logger.Error("Failed to delete deployment", zap.Error(err), zap.String("id", id))
        return fmt.Errorf("failed to delete deployment: %w", err)
    }
    m.logger.Info("Deleted deployment", zap.String("id", id), zap.Int64("deleted", result.DeletedCount))
    return nil
}

// ListDeployments retrieves all deployments in a namespace
func (m *MongoDBClient) ListDeployments(ctx context.Context, namespace string) ([]*DeploymentStatus, error) {
    filter := bson.M{"namespace": namespace}
    cursor, err := m.collection.Find(ctx, filter)
    if err != nil {
        m.logger.Error("Failed to list deployments", zap.Error(err), zap.String("namespace", namespace))
        return nil, fmt.Errorf("failed to list deployments: %w", err)
    }
    defer cursor.Close(ctx)

    var deployments []*DeploymentStatus
    for cursor.Next(ctx) {
        var dep DeploymentStatus
        if err := cursor.Decode(&dep); err != nil {
            m.logger.Error("Failed to decode deployment", zap.Error(err))
            continue
        }
        deployments = append(deployments, &dep)
    }
    if err := cursor.Err(); err != nil {
        m.logger.Error("Cursor error", zap.Error(err))
        return nil, fmt.Errorf("cursor error: %w", err)
    }
    return deployments, nil
}