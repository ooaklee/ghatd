package repositoryhelpers

import (
	"context"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Handler deals with managing mongo connection
type Handler struct {
	ConnectionString string
	DB               string
	Timeout          time.Duration
}

/*
	Used to create a singleton object of MongoDB client.

Initialized and exposed through  GetMongoClient().
*/
var clientInstance *mongo.Client

// Used during creation of singleton client object in GetMongoClient().
var clientInstanceError error

// Used to execute client creation procedure only once.
var mongoOnce sync.Once

// NewHandler returns a new Mongo handler
func NewHandler(connectionURI string, database string) *Handler {

	return &Handler{
		ConnectionString: connectionURI,
		DB:               database,
	}
}

// GetClient returns a singleton Initialized
func (h *Handler) GetClient(ctx context.Context) (*mongo.Client, error) {

	//Perform connection creation operation only once.
	mongoOnce.Do(func() {

		// NICE_TO_HAVE: Set up observability platform monitor
		//  • New Relic
		//     - newRelicMonitor := nrmongo.NewCommandMonitor(nil)
		//     - clientOptions := options.Client().ApplyURI(h.ConnectionString).SetMonitor(newRelicMonitor)

		// Set client options
		clientOptions := options.Client().ApplyURI(h.ConnectionString)
		// Connect to MongoDB
		client, err := mongo.Connect(context.TODO(), clientOptions)
		if err != nil {
			clientInstanceError = err
		}
		// Check the connection
		err = client.Ping(context.TODO(), nil)
		if err != nil {
			clientInstanceError = err
		}
		clientInstance = client
	})
	return clientInstance, clientInstanceError
}
