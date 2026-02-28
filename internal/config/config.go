package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds all OpenStack configuration.
type Config struct {
	// Host is the address the API server binds to.
	Host string
	// Port is the port the API server listens on.
	Port int
	// DataDir is the directory for storing function code and state.
	DataDir string
	// DockerHost is the Docker daemon socket path.
	DockerHost string
	// LambdaKeepAliveMS is how long warm containers stay alive (milliseconds).
	LambdaKeepAliveMS int
	// LambdaDefaultTimeout is the default function timeout in seconds.
	LambdaDefaultTimeout int
	// LambdaDefaultMemory is the default function memory in MB.
	LambdaDefaultMemory int
	// Region is the emulated AWS region.
	Region string
	// AccountID is the emulated AWS account ID.
	AccountID string
	// UIEnabled enables the built-in dashboard UI.
	UIEnabled bool
	// UIDir is the filesystem path to the built UI assets.
	UIDir string
	// PersistenceEnabled controls whether non-Lambda service state is restored across sessions.
	PersistenceEnabled bool
	// LogsMaxEventsPerGroup is the ring-buffer size per log group.
	LogsMaxEventsPerGroup int
	// LogsPersistToDisk enables writing log events to disk.
	LogsPersistToDisk bool
	// InfraProbeEnabled enables infrastructure connectivity probing.
	InfraProbeEnabled bool
	// InfraProbeTargets is a comma-separated list of "kind:host:port" targets.
	InfraProbeTargets string
}

// Default returns a Config with sensible defaults.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Host:                  "0.0.0.0",
		Port:                  4566,
		DataDir:               filepath.Join(home, ".openstack", "data"),
		DockerHost:            "unix:///var/run/docker.sock",
		LambdaKeepAliveMS:     600000, // 10 minutes
		LambdaDefaultTimeout:  3,
		LambdaDefaultMemory:   128,
		Region:                "us-east-1",
		AccountID:             "000000000000",
		UIEnabled:             false,
		UIDir:                 "./ui/build",
		PersistenceEnabled:    false,
		LogsMaxEventsPerGroup: 10000,
		LogsPersistToDisk:     false,
		InfraProbeEnabled:     true,
		InfraProbeTargets:     "postgresql:localhost:5432,redis:localhost:6379,mysql:localhost:3306",
	}
}

// LoadFromEnv overrides config values from environment variables.
func (c *Config) LoadFromEnv() {
	if v := os.Getenv("OPENSTACK_HOST"); v != "" {
		c.Host = v
	}
	if v := os.Getenv("OPENSTACK_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Port = port
		}
	}
	if v := os.Getenv("OPENSTACK_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		c.DockerHost = v
	}
	if v := os.Getenv("OPENSTACK_LAMBDA_KEEPALIVE_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			c.LambdaKeepAliveMS = ms
		}
	}
	if v := os.Getenv("OPENSTACK_REGION"); v != "" {
		c.Region = v
	}
	if v := os.Getenv("OPENSTACK_ACCOUNT_ID"); v != "" {
		c.AccountID = v
	}
	if v := os.Getenv("OPENSTACK_UI_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.UIEnabled = enabled
		}
	}
	if v := os.Getenv("OPENSTACK_UI_DIR"); v != "" {
		c.UIDir = v
	}
	if v := os.Getenv("OPENSTACK_PERSIST"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.PersistenceEnabled = enabled
		}
	}
	if v := os.Getenv("OPENSTACK_LOGS_MAX_EVENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.LogsMaxEventsPerGroup = n
		}
	}
	if v := os.Getenv("OPENSTACK_LOGS_PERSIST"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.LogsPersistToDisk = b
		}
	}
	if v := os.Getenv("OPENSTACK_INFRA_PROBE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.InfraProbeEnabled = b
		}
	}
	if v := os.Getenv("OPENSTACK_INFRA_TARGETS"); v != "" {
		c.InfraProbeTargets = v
	}
}

// EnsureDataDir creates the data directory if it doesn't exist.
func (c *Config) EnsureDataDir() error {
	return os.MkdirAll(c.DataDir, 0755)
}

// FunctionsDir returns the path where function code is stored.
func (c *Config) FunctionsDir() string {
	return filepath.Join(c.DataDir, "functions")
}

// LayersDir returns the path where layer data is stored.
func (c *Config) LayersDir() string {
	return filepath.Join(c.DataDir, "layers")
}

// QueuesDir returns the path where queue data is stored.
func (c *Config) QueuesDir() string {
	return filepath.Join(c.DataDir, "queues")
}

// QueuesStatePath returns the state snapshot path for SQS resources.
func (c *Config) QueuesStatePath() string {
	return filepath.Join(c.QueuesDir(), "state.json")
}

// SecretsDir returns the path where secrets state is stored.
func (c *Config) SecretsDir() string {
	return filepath.Join(c.DataDir, "secrets")
}

// SecretsStatePath returns the state snapshot path for Secrets Manager resources.
func (c *Config) SecretsStatePath() string {
	return filepath.Join(c.SecretsDir(), "state.json")
}

// APIGatewayDir returns the path where API Gateway state is stored.
func (c *Config) APIGatewayDir() string {
	return filepath.Join(c.DataDir, "apigateway")
}

// APIGatewayStatePath returns the state snapshot path for API Gateway resources.
func (c *Config) APIGatewayStatePath() string {
	return filepath.Join(c.APIGatewayDir(), "state.json")
}

// Endpoint returns the full API endpoint URL.
func (c *Config) Endpoint() string {
	return fmt.Sprintf("http://%s:%d", c.Host, c.Port)
}
