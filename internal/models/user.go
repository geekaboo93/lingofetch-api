package models

import "time"

const DefaultDatabaseName = "LingoFetch Dictionary"
const DefaultObsidianVaultName = "LingoFetch Dictionary"

type LanguageRoute struct {
	DatabaseID   string `json:"database_id" firestore:"database_id"`
	DatabaseName string `json:"database_name" firestore:"database_name"`
	LanguageName string `json:"language_name,omitempty" firestore:"language_name,omitempty"`
}

// NotionConfig stores user-specific Notion integration details
type NotionConfig struct {
	AccessToken         string                   `firestore:"access_token" json:"-"`
	DefaultDatabaseID   string                   `firestore:"default_database_id" json:"default_database_id"`
	DefaultDatabaseName string                   `firestore:"default_database_name" json:"default_database_name"`
	WorkspaceName       string                   `firestore:"workspace_name" json:"workspace_name"`
	WorkspaceIcon       string                   `firestore:"workspace_icon" json:"workspace_icon"`
	ParentPageID        string                   `firestore:"parent_page_id" json:"parent_page_id"`
	DetectedLanguages   map[string]LanguageRoute `firestore:"detected_languages" json:"detected_languages"`
	ConnectedAt         time.Time                `firestore:"connected_at" json:"connected_at"`
}

// ObsidianConfig stores user-specific Obsidian integration details (Local REST API)
type ObsidianConfig struct {
	AccessToken         string                   `firestore:"access_token" json:"-"`
	BaseURL             string                   `firestore:"base_url" json:"base_url"`
	DefaultDatabaseID   string                   `firestore:"default_database_id" json:"default_database_id"`
	DefaultDatabaseName string                   `firestore:"default_database_name" json:"default_database_name"`
	DetectedLanguages   map[string]LanguageRoute `firestore:"detected_languages" json:"detected_languages"`
	ConnectedAt         time.Time                `firestore:"connected_at" json:"connected_at"`
}

// NotesConfig groups all note-taking application settings
type NotesConfig struct {
	ActiveProvider string          `firestore:"active_provider" json:"active_provider"` // "notion" or "obsidian"
	Notion         *NotionConfig   `firestore:"notion" json:"notion"`
	Obsidian       *ObsidianConfig `firestore:"obsidian" json:"obsidian"`
}

// AIPreferences stores user-specific AI configuration
type AIPreferences struct {
	DefaultProvider string `firestore:"default_provider" json:"default_provider"`
	TargetLanguage  string `firestore:"target_language" json:"target_language"`
	CustomAPIKey    string `firestore:"custom_api_key" json:"-"`
}

// User represents the root user document in Firestore
type User struct {
	ID        string         `firestore:"id" json:"id"`
	Email     string         `firestore:"email" json:"email"`
	Notes     *NotesConfig   `firestore:"notes" json:"notes"`
	AIPrefs   *AIPreferences `firestore:"ai_preferences" json:"ai_preferences"`
	CreatedAt time.Time      `firestore:"created_at" json:"created_at"`
	UpdatedAt time.Time      `firestore:"updated_at" json:"updated_at"`
}
