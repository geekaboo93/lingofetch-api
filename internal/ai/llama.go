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
	models     []string
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
func NewLlamaService(ctx context.Context, apiKey string, models []string) (*LlamaService, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Llama API key is required")
	}

	if len(models) == 0 {
		models = []string{
			"meta-llama/llama-3.3-70b-instruct:free",
			"meta-llama/llama-3.1-405b-instruct:free",
			"openai/gpt-oss-120b:free",
			"qwen/qwen3-coder:free",
			"deepseek/deepseek-chat:free",
			"openai/gpt-oss-20b:free",
			"qwen/qwen-2.5-vl-7b-instruct:free",
			"meta-llama/llama-3.2-3b-instruct:free",
		}
	}

	return &LlamaService{
		apiKey:     apiKey,
		httpClient: &http.Client{},
		models:     models,
	}, nil
}

// GenerateDefinition generates a definition by racing multiple free models for speed and reliability
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

	freeModels := s.models

	// Channel to receive the first successful result
	resultChan := make(chan *WordAnalysis, len(freeModels))
	errChan := make(chan error, len(freeModels))

	// Create a cancellable context to stop other requests once one succeeds
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, model := range freeModels {
		go func(m string) {
			analysis, err := s.callModel(raceCtx, m, prompt, word)
			if err != nil {
				// Don't log context cancellation as a "real" error
				if raceCtx.Err() == nil {
					fmt.Printf("[AI] Racing: Model %s failed: %v\n", m, err)
					errChan <- err
				}
				return
			}
			resultChan <- analysis
		}(model)
	}

	// Wait for first success or all failures
	failedCount := 0
	for {
		select {
		case result := <-resultChan:
			cancel() // Stop all other requests
			return result, nil
		case <-errChan:
			failedCount++
			if failedCount >= len(freeModels) {
				return nil, fmt.Errorf("all raced models failed to generate content")
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *LlamaService) callModel(ctx context.Context, model, prompt, originalWord string) (*WordAnalysis, error) {
	reqBody := llamaRequest{
		Model: model,
		Messages: []llamaMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := "https://openrouter.ai/api/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.apiKey))
	req.Header.Set("HTTP-Referer", "https://github.com/geekabo93/lingofetch")
	req.Header.Set("X-Title", "LingoFetch")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var llamaResp llamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&llamaResp); err != nil {
		return nil, err
	}

	if len(llamaResp.Choices) == 0 {
		return nil, fmt.Errorf("no content")
	}

	text := llamaResp.Choices[0].Message.Content
	fmt.Printf("[AI] Model %s won the race!\n", model)
	return parseDefinition(text, originalWord)
}

// Name returns the provider name
func (s *LlamaService) Name() string {
	return "llama"
}

// Close closes the HTTP client (no-op for standard client)
func (s *LlamaService) Close() error {
	return nil
}
