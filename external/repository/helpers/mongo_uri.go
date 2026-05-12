package repositoryhelpers

import (
	"fmt"
	"net/url"
	"strings"
)

// MongoURIConfig holds the values needed to build a MongoDB connection URI.
type MongoURIConfig struct {
	Username string
	Password string
	Host     string
	AppName  string
	Atlas    bool
}

// GenerateMongoURI builds either a standard MongoDB URI or an Atlas SRV URI.
func GenerateMongoURI(config MongoURIConfig) (string, error) {
	if strings.TrimSpace(config.Host) == "" {
		return "", fmt.Errorf("mongo-host-cannot-be-empty")
	}
	if strings.TrimSpace(config.Username) == "" && config.Password != "" {
		return "", fmt.Errorf("mongo-username-cannot-be-empty-when-password-is-set")
	}

	if config.Atlas {
		return GenerateAtlasMongoURI(config.Username, config.Password, config.Host, config.AppName), nil
	}

	return GenerateGenericMongoURI(config.Username, config.Password, config.Host), nil
}

// GenerateGenericMongoURI builds a standard MongoDB URI.
// Use GenerateMongoURI when caller input needs validation.
func GenerateGenericMongoURI(databaseUsername, databasePassword, databaseHost string) string {
	return buildMongoURI("mongodb", databaseUsername, databasePassword, databaseHost, nil)
}

// GenerateAtlasMongoURI builds a MongoDB Atlas SRV URI with Atlas defaults.
// Use GenerateMongoURI when caller input needs validation.
func GenerateAtlasMongoURI(databaseUsername, databasePassword, databaseHost, databaseAppName string) string {
	query := url.Values{}
	query.Set("retryWrites", "true")
	query.Set("w", "majority")
	if databaseAppName != "" {
		query.Set("appName", databaseAppName)
	}

	return buildMongoURI("mongodb+srv", databaseUsername, databasePassword, databaseHost, query)
}

// buildMongoURI composes a MongoDB URI from its scheme, credentials, host, and query parameters.
func buildMongoURI(scheme, username, password, host string, query url.Values) string {
	uri := url.URL{
		Scheme: scheme,
		Host:   host,
	}
	if username != "" || password != "" {
		uri.User = url.UserPassword(username, password)
	}
	if len(query) > 0 {
		uri.Path = "/"
		uri.RawQuery = query.Encode()
	}

	return uri.String()
}
