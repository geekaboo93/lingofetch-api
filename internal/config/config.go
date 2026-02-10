package config

import (
	"context"
	"fmt"
	"os"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	Port             string
	GeminiAPIKey     string
	OpenRouterAPIKey string

	NotionAPIKey        string
	NotionDatabaseID    string
	GCPProjectID        string
	FirestoreDatabaseID string
	Environment         string
	AIProvider          string

	// Notion OAuth Configuration
	NotionClientID     string
	NotionClientSecret string
	NotionRedirectURI  string

	// Security
	EncryptionKey string // 32-byte hex for AES-256

	// AI Models
	GeminiModels     []string
	OpenRouterModels []string
}

// Load initializes configuration from environment variables and GCP Secret Manager
func Load() (*Config, error) {
	// Load .env file if it exists (for local development)
	_ = godotenv.Load()

	cfg := &Config{
		Port:                getEnv("PORT", "8080"),
		GCPProjectID:        getEnv("GCP_PROJECT_ID", ""),
		FirestoreDatabaseID: getEnv("FIRESTORE_DATABASE_ID", "(default)"),
		Environment:         getEnv("ENVIRONMENT", "development"),
		GeminiAPIKey:        getEnv("GEMINI_API_KEY", ""),
		OpenRouterAPIKey:    getEnv("OPENROUTER_API_KEY", ""),

		NotionAPIKey:       getEnv("NOTION_API_KEY", ""),
		NotionDatabaseID:   getEnv("NOTION_DATABASE_ID", ""),
		AIProvider:         getEnv("AI_PROVIDER", "openrouter"),
		NotionClientID:     getEnv("NOTION_OAUTH_CLIENT_ID", ""),
		NotionClientSecret: getEnv("NOTION_OAUTH_CLIENT_SECRET", ""),
		NotionRedirectURI:  getEnv("NOTION_OAUTH_REDIRECT_URI", ""),
		EncryptionKey:      getEnv("ENCRYPTION_KEY", ""),

		GeminiModels:     getEnvList("GEMINI_MODELS", "gemini-2.5-flash,gemini-2.0-flash,gemini-1.5-flash,gemini-1.5-pro"),
		OpenRouterModels: getEnvList("OPENROUTER_MODELS", "meta-llama/llama-3.3-70b-instruct:free,meta-llama/llama-3.1-405b-instruct:free,openai/gpt-oss-120b:free,qwen/qwen3-coder:free,deepseek/deepseek-chat:free,openai/gpt-oss-20b:free,qwen/qwen-2.5-vl-7b-instruct:free,meta-llama/llama-3.2-3b-instruct:free"),
	}

	// If running in GCP (production), fetch secrets from Secret Manager
	if cfg.Environment == "production" && cfg.GCPProjectID != "" {
		if err := cfg.loadSecretsFromGCP(); err != nil {
			return nil, fmt.Errorf("failed to load secrets from GCP: %w", err)
		}
	}

	// Validate required configuration (Minimalist validation for dev)
	if err := cfg.Validate(); err != nil {
		fmt.Printf("Warning: Configuration validation failed: %v\n", err)
	}

	return cfg, nil
}

// loadSecretsFromGCP fetches secrets from GCP Secret Manager
func (c *Config) loadSecretsFromGCP() error {
	ctx := context.Background()
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create secret manager client: %w", err)
	}
	defer client.Close()

	// Fetch API Keys
	c.GeminiAPIKey, _ = accessSecret(ctx, client, c.GCPProjectID, "gemini-key")
	c.OpenRouterAPIKey, _ = accessSecret(ctx, client, c.GCPProjectID, "openrouter-key")
	c.NotionAPIKey, _ = accessSecret(ctx, client, c.GCPProjectID, "notion-key")
	c.NotionDatabaseID, _ = accessSecret(ctx, client, c.GCPProjectID, "notion-db")
	c.FirestoreDatabaseID, _ = accessSecret(ctx, client, c.GCPProjectID, "firestore-db")

	// Fetch OAuth Secrets
	c.NotionClientID, _ = accessSecret(ctx, client, c.GCPProjectID, "notion-client-id")
	c.NotionClientSecret, _ = accessSecret(ctx, client, c.GCPProjectID, "notion-client-secret")
	c.EncryptionKey, _ = accessSecret(ctx, client, c.GCPProjectID, "encryption-key")

	return nil
}

// accessSecret retrieves a secret from GCP Secret Manager
func accessSecret(ctx context.Context, client *secretmanager.Client, projectID, secretName string) (string, error) {
	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName),
	}

	result, err := client.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", err
	}

	return string(result.Payload.Data), nil
}

// Validate checks if all required configuration is present
func (c *Config) Validate() error {
	if c.Environment == "production" {
		if c.NotionClientID == "" || c.NotionClientSecret == "" {
			return fmt.Errorf("Notion OAuth credentials are required in production")
		}
		if c.EncryptionKey == "" {
			return fmt.Errorf("ENCRYPTION_KEY is required in production")
		}
	}
	return nil
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvList retrieves an environment variable and splits it by comma into a slice
func getEnvList(key, defaultValue string) []string {
	val := getEnv(key, defaultValue)
	if val == "" {
		return []string{}
	}
	parts := strings.Split(val, ",")
	var result []string
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
