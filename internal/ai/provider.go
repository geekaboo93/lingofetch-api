package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Provider defines the interface that all AI providers must implement
type Provider interface {
	// GenerateDefinition generates a comprehensive definition for a word in the target language
	GenerateDefinition(ctx context.Context, word, targetLanguage, contextStr, pageLanguage, sourceLanguage string) (*WordAnalysis, error)

	// Close cleans up any resources
	Close() error

	// Name returns the provider name
	Name() string
}

// ProviderType represents the type of AI provider
type ProviderType string

const (
	ProviderGemini ProviderType = "gemini"
	ProviderLlama  ProviderType = "llama"
)

// WordAnalysis represents the AI-generated linguistic data
type WordAnalysis struct {
	Word              string   `json:"word"`
	Definition        string   `json:"definition"`
	Pronunciation     string   `json:"pronunciation"`
	PartOfSpeech      string   `json:"part_of_speech"`
	Example           string   `json:"example"`
	LanguageCode      string   `json:"language_code"`
	IsAmbiguous       bool     `json:"is_ambiguous"`
	PossibleLanguages []string `json:"possible_languages"`
}

// parseDefinition extracts structured data from AI response
func parseDefinition(text, originalWord string) (*WordAnalysis, error) {
	// 1. Robust Extraction: Find the JSON block even if there is extra text
	startIdx := strings.Index(text, "{")
	endIdx := strings.LastIndex(text, "}")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		text = text[startIdx : endIdx+1]
	}

	// 2. Fix Hallucinated Escapes: Some small models (like Llama 3B) hallucinate invalid \u escapes
	// e.g., \uneysyanu. We'll try to escape these so the JSON parser doesn't crash.
	// We use a byte slice to avoid corrupting multibyte UTF-8 characters.
	fixedBytes := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) && text[i+1] == 'u' {
			// Check if followed by 4 hex digits
			isHex := true
			if i+5 >= len(text) {
				isHex = false
			} else {
				for j := 2; j <= 5; j++ {
					c := text[i+j]
					if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
						isHex = false
						break
					}
				}
			}
			if !isHex {
				fixedBytes = append(fixedBytes, '\\', '\\') // Double escape the backslash
				continue
			}
		}
		fixedBytes = append(fixedBytes, text[i])
	}
	text = string(fixedBytes)

	var analysis WordAnalysis
	if err := json.Unmarshal([]byte(text), &analysis); err != nil {
		fmt.Printf("[AI] JSON Parse Error: %v\nRaw Text: %s\n", err, text)
		return nil, fmt.Errorf("failed to parse AI response as JSON: %w", err)
	}

	// Validate essential fields
	if analysis.Definition == "" {
		return nil, fmt.Errorf("AI response missing definition")
	}

	// Set defaults/fallbacks
	// If AI didn't provide a word, or provided a truncated version (common in CJK), use original
	if analysis.Word == "" || (len(originalWord) > len(analysis.Word) && strings.Contains(originalWord, analysis.Word)) {
		analysis.Word = originalWord
	}
	if analysis.Pronunciation == "" {
		analysis.Pronunciation = "N/A"
	}
	if analysis.PartOfSpeech == "" {
		analysis.PartOfSpeech = "unknown"
	}
	if analysis.Example == "" {
		analysis.Example = fmt.Sprintf("Example usage of '%s' not available.", analysis.Word)
	}

	return &analysis, nil
}
