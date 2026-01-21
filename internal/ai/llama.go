package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/geekabo93/lingofetch/internal/pkg/resources"
)

// LlamaService handles AI-powered definition generation using Llama (via OpenRouter)
type LlamaService struct {
	apiKey     string
	httpClient *http.Client
}

// Ensure LlamaService implements Provider interface
var _ Provider = (*LlamaService)(nil)

// llamaRequest represents the request structure for Llama API
type llamaRequest struct {
	Model    string         `json:"model"`
	Messages []llamaMessage `json:"messages"`
}

type llamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llamaResponse represents the response structure from Llama API
type llamaResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewLlamaService creates a new Llama AI service
func NewLlamaService(ctx context.Context, apiKey string) (*LlamaService, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Llama API key is required")
	}

	return &LlamaService{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}, nil
}

// GenerateDefinition generates a comprehensive definition for a word using Llama in the target language
func (s *LlamaService) GenerateDefinition(ctx context.Context, word, targetLanguage, contextStr, pageLanguage, sourceLanguage string) (*WordAnalysis, error) {
	prompt, err := resources.GetPrompt("definition", map[string]string{
		"Word":           word,
		"TargetLanguage": targetLanguage,
		"Context":        contextStr,
		"PageLanguage":   pageLanguage,
		"SourceLanguage": sourceLanguage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt: %w", err)
	}

	// Prepare request
	reqBody := llamaRequest{
		Model: "meta-llama/llama-3.3-70b-instruct:free",
		Messages: []llamaMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make API call (Using OpenRouter for the free model tier)
	url := "https://openrouter.ai/api/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.apiKey))
	req.Header.Set("HTTP-Referer", "https://github.com/geekabo93/lingofetch") // Required by OpenRouter
	req.Header.Set("X-Title", "LingoFetch")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Llama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Llama API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var llamaResp llamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&llamaResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(llamaResp.Choices) == 0 {
		return nil, fmt.Errorf("no content generated")
	}

	text := llamaResp.Choices[0].Message.Content
	fmt.Printf("[Debug] [Llama] Raw Response: %s\n", text)
	return parseDefinition(text, word)
}

// Name returns the provider name
func (s *LlamaService) Name() string {
	return "llama"
}

// Close closes the HTTP client (no-op for standard client)
func (s *LlamaService) Close() error {
	return nil
}
