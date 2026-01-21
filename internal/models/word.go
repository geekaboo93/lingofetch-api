package models

import "time"

// WordEntry represents a captured word with its linguistic data
type WordEntry struct {
	ID             string    `json:"id" firestore:"id"`
	Word           string    `json:"word" firestore:"word"`
	Definition     string    `json:"definition" firestore:"definition"`
	Pronunciation  string    `json:"pronunciation" firestore:"pronunciation"`
	PartOfSpeech   string    `json:"part_of_speech" firestore:"part_of_speech"`
	Example        string    `json:"example" firestore:"example"`
	TargetLanguage string    `json:"target_language,omitempty" firestore:"target_language,omitempty"`
	SourceLanguage string    `json:"source_language,omitempty" firestore:"source_language,omitempty"`
	NotionURL      string    `json:"notion_url,omitempty" firestore:"notion_url,omitempty"`
	CreatedAt      time.Time `json:"created_at" firestore:"created_at"`
}

// CaptureRequest represents the API request payload
type CaptureRequest struct {
	Word           string `json:"word" binding:"required"`
	Provider       string `json:"provider,omitempty"`        // gemini, llama, openai, claude, grok, deepseek, openrouter
	TargetLanguage string `json:"target_language,omitempty"` // en-US, zh-CN, etc.
	UserID         string `json:"user_id,omitempty"`         // Unique ID from the extension
	Context        string `json:"context,omitempty"`         // Surrounding text
	PageLanguage   string `json:"page_language,omitempty"`   // Language from HTML/Chrome API
	SourceLanguage string `json:"source_language,omitempty"` // User selected language after ambiguity
	Timestamp      string `json:"timestamp,omitempty"`       // Local timestamp from client
	CustomAPIKey   string `json:"custom_api_key,omitempty"`  // Optional: User's own API key (not stored)
}

// CaptureResponse represents the API response payload
type CaptureResponse struct {
	Success           bool      `json:"success"`
	Word              string    `json:"word"`
	Definition        string    `json:"definition"`
	Pronunciation     string    `json:"pronunciation"`
	PartOfSpeech      string    `json:"part_of_speech"`
	Example           string    `json:"example"`
	NotionURL         string    `json:"notion_url"`
	Message           string    `json:"message,omitempty"`
	IsDuplicate       bool      `json:"is_duplicate"`
	Timestamp         time.Time `json:"timestamp"`
	SourceLanguage    string    `json:"source_language,omitempty"`
	SuggestRouting    bool      `json:"suggest_routing,omitempty"`
	HoldSync          bool      `json:"hold_sync,omitempty"`
	IsAmbiguous       bool      `json:"is_ambiguous,omitempty"`
	PossibleLanguages []string  `json:"possible_languages,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}
