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

// Deployment represents a deployment record
// type Deployment struct {
// 	ID         string    `bson:"_id"`
// 	Name       string    `bson:"name"`
// 	Strategy   string    `bson:"strategy"`
// 	Namespace  string    `bson:"namespace"`
// 	Status     string    `bson:"status"`
// 	CreatedAt  time.Time `bson:"created_at"`
// 	UpdatedAt  time.Time `bson:"updated_at"`
// 	Replicas   int32     `bson:"replicas"`
// 	// Rollback   Rollback  `bson:"rollback"`
// }

// Leave empty for now, we can add fields later
// type Rollback struct {}

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

// SaveDeployment saves or updates a deployment record
func (m *MongoDBClient) SaveDeployment(ctx context.Context, dep *DeploymentStatus) error {
	// dep.StartTime = time.Now()
	// if dep.StartTime.IsZero() {
	// 	dep.CreatedAt = dep.UpdatedAt
	// }

	filter := bson.M{"_id": dep.ID}
	update := bson.M{
		"$set": dep,
	}
	opts := options.Update().SetUpsert(true)

	result, err := m.collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		m.logger.Error("Failed to save deployment", zap.Error(err), zap.String("id", dep.ID))
		return fmt.Errorf("failed to save deployment: %w", err)
	}
	m.logger.Info("Saved deployment", zap.String("id", dep.ID), zap.Int64("modified", result.ModifiedCount))
	return nil
}

// GetDeployment retrieves a deployment by ID
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

// DeleteDeployment deletes a deployment by ID
func (m *MongoDBClient) DeleteDeployment(ctx context.Context, id string) (error) {
	filter := bson.M{"_id": id}
	result , err := m.collection.DeleteOne(ctx, filter)
	if err != nil {
		m.logger.Error("Failed to delete deployment", zap.Error(err), zap.String("id", id))
		return fmt.Errorf("failed to delete deployment: %w", err)
		
	}
	m.logger.Info("Deleted deployment", zap.String("id", id), zap.Int64("modified", result.DeletedCount))
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


func(m *MongoDBClient) GetDeploymentByName(ctx context.Context, namespace, name string) (*DeploymentStatus, error) {
	filter := bson.M{"namespace": namespace, "app_name": name}
	if namespace == "" {
		filter = bson.M{"app_name":name}
	}
	var dep DeploymentStatus
	err := m.collection.FindOne(ctx, filter).Decode(&dep)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		m.logger.Error("Failed to get deployment by name", zap.Error(err), zap.String("namespace", namespace), zap.String("name", name))
		return nil, fmt.Errorf("failed to get deployment by name: %w", err)
	}
	return &dep, nil
}