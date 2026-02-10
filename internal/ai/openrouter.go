package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/geekabo93/lingofetch/internal/pkg/resources"
)

// OpenRouterService handles AI-powered definition generation using multiple models (via OpenRouter)
type OpenRouterService struct {
	apiKey     string
	httpClient *http.Client
	models     []string
}

// Ensure OpenRouterService implements Provider interface
var _ Provider = (*OpenRouterService)(nil)

// openRouterRequest represents the request structure for OpenRouter API
type openRouterRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openRouterResponse represents the response structure from OpenRouter API
type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewOpenRouterService creates a new OpenRouter AI service
func NewOpenRouterService(ctx context.Context, apiKey string, models []string) (*OpenRouterService, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OpenRouter API key is required")
	}

	if len(models) == 0 {
		models = []string{
			"meta-llama/llama-3.3-70b-instruct:free",
			"meta-llama/llama-3.1-405b-instruct:free",
			"google/gemini-2.5-pro-exp-03-25:free",
			"mistralai/mistral-small-3.1-24b-instruct:free",
			"deepseek/deepseek-r1-zero:free",
			"qwen/qwen3-coder-480b-a35b:free",
			"upstage/solar-pro-3:free",
			"google/gemma-3-27b:free",
			"meta-llama/llama-3.2-3b-instruct:free",
		}
	}

	return &OpenRouterService{
		apiKey:     apiKey,
		httpClient: &http.Client{},
		models:     models,
	}, nil
}

// GenerateDefinition generates a definition by racing multiple free models for speed and reliability
func (s *OpenRouterService) GenerateDefinition(ctx context.Context, word, targetLanguage, contextStr, pageLanguage, sourceLanguage string) (*WordAnalysis, error) {
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

	// Create a cancellable context with a 15s timeout for the racing phase
	// This ensures we return quickly enough for fallbacks to work before the client times out
	raceCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	for _, model := range freeModels {
		go func(m string) {
			analysis, err := s.callModel(raceCtx, m, prompt, word)
			if err != nil {
				// Don't log context cancellation or timeout as a "real" error
				if raceCtx.Err() == nil {
					fmt.Printf("[AI] Racing: Model %s failed: %v\n", m, err)
					errChan <- err
				}
				return
			}
			resultChan <- analysis
		}(model)
		// Small delay to prevent hitting burst rate limits
		time.Sleep(100 * time.Millisecond)
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
		case <-raceCtx.Done():
			// If we timed out or the parent context was canceled
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("openrouter racing timed out after 15s")
		}
	}
}

func (s *OpenRouterService) callModel(ctx context.Context, model, prompt, originalWord string) (*WordAnalysis, error) {
	reqBody := openRouterRequest{
		Model: model,
		Messages: []openRouterMessage{
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

	var orResp openRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&orResp); err != nil {
		return nil, err
	}

	if len(orResp.Choices) == 0 {
		return nil, fmt.Errorf("no content")
	}

	text := orResp.Choices[0].Message.Content
	fmt.Printf("[AI] Model %s won the race!\n", model)
	return parseDefinition(text, originalWord)
}

// Name returns the provider name
func (s *OpenRouterService) Name() string {
	return "openrouter"
}

// Close closes the HTTP client (no-op for standard client)
func (s *OpenRouterService) Close() error {
	return nil
}
