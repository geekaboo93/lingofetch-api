package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ProviderInfo represents metadata about a supported note provider
type ProviderInfo struct {
	ID          string `json:"id"`          // e.g., "notion", "obsidian"
	Name        string `json:"name"`        // e.g., "Notion", "Obsidian"
	Description string `json:"description"` // e.g., "Connected via official Notion API"
	Icon        string `json:"icon"`        // Icon key or URL
	AuthType    string `json:"auth_type"`   // e.g., "oauth2", "api_key"
}

// DiscoveryHandler handles system discovery requests
type DiscoveryHandler struct{}

// NewDiscoveryHandler creates a new discovery handler
func NewDiscoveryHandler() *DiscoveryHandler {
	return &DiscoveryHandler{}
}

// GetSupportedNoteProviders returns a list of all note-taking apps supported by the backend
func (h *DiscoveryHandler) GetSupportedNoteProviders(c *gin.Context) {
	providers := []ProviderInfo{
		{
			ID:          "notion",
			Name:        "Notion",
			Description: "Save words to a structured Notion database.",
			Icon:        "notion",
			AuthType:    "oauth2",
		},
		{
			ID:          "obsidian",
			Name:        "Obsidian",
			Description: "Save words locally to your Obsidian vault via Local REST API.",
			Icon:        "obsidian",
			AuthType:    "api_key",
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"note_providers": providers,
	})
}
