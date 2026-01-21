package ai

import (
	"context"
	"fmt"
)

// ProviderConfig holds configuration for creating an AI provider
type ProviderConfig struct {
	Type   ProviderType
	APIKey string
}

// NewProvider creates a new AI provider based on the configuration
func NewProvider(ctx context.Context, config ProviderConfig) (Provider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("API key is required for %s provider", config.Type)
	}

	switch config.Type {
	case ProviderGemini:
		return NewGeminiService(ctx, config.APIKey)
	case ProviderLlama:
		return NewLlamaService(ctx, config.APIKey)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", config.Type)
	}
}

// GetProviderFromString converts a string to ProviderType
func GetProviderFromString(provider string) (ProviderType, error) {
	switch provider {
	case "gemini":
		return ProviderGemini, nil
	case "llama":
		return ProviderLlama, nil
	case "openai":
		return "openai", nil
	case "claude":
		return "claude", nil
	case "grok":
		return "grok", nil
	case "deepseek":
		return "deepseek", nil
	case "openrouter":
		return ProviderLlama, nil // OpenRouter uses same interface as Llama
	default:
		return "", fmt.Errorf("unknown provider: %s (supported: gemini, llama, openai, claude, grok, deepseek, openrouter)", provider)
	}
}
