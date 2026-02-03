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

// GeminiService handles AI-powered definition generation using Gemini
type GeminiService struct {
	apiKey     string
	httpClient *http.Client
}

// Ensure GeminiService implements Provider interface
var _ Provider = (*GeminiService)(nil)

// geminiRequest represents the request structure for Gemini API
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// geminiResponse represents the response structure from Gemini API
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// NewGeminiService creates a new Gemini AI service
func NewGeminiService(ctx context.Context, apiKey string) (*GeminiService, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API key is required")
	}

	return &GeminiService{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}, nil
}

// GenerateDefinition generates a comprehensive definition for a word using Gemini in the target language
func (s *GeminiService) GenerateDefinition(ctx context.Context, word, targetLanguage, contextStr, pageLanguage, sourceLanguage string) (*WordAnalysis, error) {
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
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make API call (Using Gemini 1.5 Flash for performance and stability)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=%s", s.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Gemini API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no content generated")
	}

	text := geminiResp.Candidates[0].Content.Parts[0].Text
	fmt.Printf("[Debug] [Gemini] Raw Response: %s\n", text)
	return parseDefinition(text, word)
}

// Name returns the provider name
func (s *GeminiService) Name() string {
	return "gemini"
}

// Close closes the HTTP client (no-op for standard client)
func (s *GeminiService) Close() error {
	return nil
}
