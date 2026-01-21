package models

import "time"

type LanguageRoute struct {
	DatabaseID   string `json:"database_id" firestore:"database_id"`
	DatabaseName string `json:"database_name" firestore:"database_name"`
	LanguageName string `json:"language_name,omitempty" firestore:"language_name,omitempty"`
}

// NotionConfig stores user-specific Notion integration details
type NotionConfig struct {
	AccessToken       string                   `firestore:"access_token" json:"-"`
	DatabaseID        string                   `firestore:"database_id" json:"database_id"`
	DatabaseName      string                   `firestore:"database_name" json:"database_name"`
	WorkspaceName     string                   `firestore:"workspace_name" json:"workspace_name"`
	WorkspaceIcon     string                   `firestore:"workspace_icon" json:"workspace_icon"`
	ParentPageID      string                   `firestore:"parent_page_id" json:"parent_page_id"`
	DetectedLanguages map[string]LanguageRoute `firestore:"detected_languages" json:"detected_languages"`
	ConnectedAt       time.Time                `firestore:"connected_at" json:"connected_at"`
}

// AIPreferences stores user-specific AI configuration
type AIPreferences struct {
	DefaultProvider string `firestore:"default_provider" json:"default_provider"`
	TargetLanguage  string `firestore:"target_language" json:"target_language"`
	CustomAPIKey    string `firestore:"custom_api_key" json:"-"`
}

// User represents the root user document in Firestore
type User struct {
	ID           string         `firestore:"id" json:"id"`
	Email        string         `firestore:"email" json:"email"`
	NotionConfig *NotionConfig  `firestore:"notion_config" json:"notion_config"`
	AIPrefs      *AIPreferences `firestore:"ai_preferences" json:"ai_preferences"`
	CreatedAt    time.Time      `firestore:"created_at" json:"created_at"`
	UpdatedAt    time.Time      `firestore:"updated_at" json:"updated_at"`
}
