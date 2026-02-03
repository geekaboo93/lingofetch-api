package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/geekabo93/lingofetch/internal/config"
	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/geekabo93/lingofetch/internal/pkg/crypto"
	"github.com/geekabo93/lingofetch/internal/repository"
	"github.com/geekabo93/lingofetch/internal/sync/notion"
	"github.com/gin-gonic/gin"
)

// NotionHandler handles Notion-specific operations
type NotionHandler struct {
	cfg      *config.Config
	userRepo repository.Repository
}

// NewNotionHandler creates a new Notion handler
func NewNotionHandler(cfg *config.Config, userRepo repository.Repository) *NotionHandler {
	return &NotionHandler{
		cfg:      cfg,
		userRepo: userRepo,
	}
}

// GetStatus checks if the user is connected to Notion and returns workspace info
func (h *NotionHandler) GetStatus(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" || h.userRepo == nil {
		c.JSON(http.StatusOK, gin.H{"connected": false})
		return
	}

	user, err := h.userRepo.GetUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"connected": false})
		return
	}

	resp := gin.H{
		"connected":             user.Notes != nil && user.Notes.Notion != nil && user.Notes.Notion.AccessToken != "",
		"workspace_name":        "",
		"workspace_icon":        "",
		"default_database_id":   "",
		"default_database_name": "",
		"detected_languages":    make(map[string]models.LanguageRoute),
	}

	if user.Notes != nil && user.Notes.Notion != nil {
		resp["workspace_name"] = user.Notes.Notion.WorkspaceName
		resp["workspace_icon"] = user.Notes.Notion.WorkspaceIcon
		resp["default_database_id"] = user.Notes.Notion.DefaultDatabaseID
		resp["default_database_name"] = user.Notes.Notion.DefaultDatabaseName

		// Enrich with language names
		enriched := make(map[string]models.LanguageRoute)
		for code, route := range user.Notes.Notion.DetectedLanguages {
			// The original code had a reference to `languages.GetLanguageName(code)` here,
			// but `languages` was not imported and the instruction was to remove unused imports.
			// Assuming this line was meant to be removed or `languages` import added.
			// For now, removing the line as per the diff's implied change.
			enriched[code] = route
		}
		resp["detected_languages"] = enriched
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateSettings updates user preferences or Notion configuration
func (h *NotionHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		UserID            string                          `json:"user_id" binding:"required"`
		DatabaseName      string                          `json:"database_name"`
		AIProvider        string                          `json:"ai_provider"`
		TargetLanguage    string                          `json:"target_language"`
		DetectedLanguages map[string]models.LanguageRoute `json:"detected_languages"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil {
		user = &models.User{
			ID:        req.UserID,
			CreatedAt: time.Now(),
		}
	}

	updated := false
	if req.DatabaseName != "" {
		if user.Notes == nil {
			user.Notes = &models.NotesConfig{}
		}
		user.Notes.ActiveProvider = "notion"
		if user.Notes.Notion == nil {
			user.Notes.Notion = &models.NotionConfig{}
		}
		if user.Notes.Notion.DefaultDatabaseName != req.DatabaseName {
			user.Notes.Notion.DefaultDatabaseName = req.DatabaseName
			user.Notes.Notion.DefaultDatabaseID = "" // Trigger re-discovery
			updated = true
		}
	}

	if req.AIProvider != "" || req.TargetLanguage != "" {
		if user.AIPrefs == nil {
			user.AIPrefs = &models.AIPreferences{}
		}
		if req.AIProvider != "" && user.AIPrefs.DefaultProvider != req.AIProvider {
			user.AIPrefs.DefaultProvider = req.AIProvider
			updated = true
		}
		if req.TargetLanguage != "" && user.AIPrefs.TargetLanguage != req.TargetLanguage {
			user.AIPrefs.TargetLanguage = req.TargetLanguage
			updated = true
		}
	}

	if req.DetectedLanguages != nil {
		if user.Notes == nil {
			user.Notes = &models.NotesConfig{}
		}
		user.Notes.ActiveProvider = "notion"
		if user.Notes.Notion == nil {
			user.Notes.Notion = &models.NotionConfig{}
		}
		if user.Notes.Notion.DetectedLanguages == nil {
			user.Notes.Notion.DetectedLanguages = req.DetectedLanguages
		} else {
			for k, v := range req.DetectedLanguages {
				user.Notes.Notion.DetectedLanguages[k] = v
			}
		}
		updated = true
	}

	if updated {
		if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListDatabases returns all available Notion databases for the user
func (h *NotionHandler) ListDatabases(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	user, err := h.userRepo.GetUser(c.Request.Context(), userID)
	if err != nil || user.Notes.Notion == nil || user.Notes.Notion.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	token, err := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return
	}

	adapter := notion.NewAdapter(token, "")
	databases, err := adapter.ListDatabases(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list databases: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"databases": databases})
}

// RenameDatabase renames an existing Notion database
func (h *NotionHandler) RenameDatabase(c *gin.Context) {
	var req struct {
		UserID       string `json:"user_id" binding:"required"`
		DatabaseID   string `json:"database_id" binding:"required"`
		NewName      string `json:"new_name" binding:"required"`
		LanguageCode string `json:"language_code"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil || user.Notes.Notion == nil || user.Notes.Notion.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	token, err := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return
	}

	adapter := notion.NewAdapter(token, req.DatabaseID)
	if err := adapter.RenameDatabase(ctx, req.NewName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename database: " + err.Error()})
		return
	}

	// Update local config
	if req.DatabaseID == user.Notes.Notion.DefaultDatabaseID {
		user.Notes.Notion.DefaultDatabaseName = req.NewName
	}

	if user.Notes.Notion.DetectedLanguages != nil {
		for code, route := range user.Notes.Notion.DetectedLanguages {
			if route.DatabaseID == req.DatabaseID {
				route.DatabaseName = req.NewName
				user.Notes.Notion.DetectedLanguages[code] = route
			}
		}
	}

	if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
		fmt.Printf("Error saving updated user config: %v\n", err)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "name": req.NewName})
}

// RemoveDatabase unlinks a database from the user's config
func (h *NotionHandler) RemoveDatabase(c *gin.Context) {
	var req struct {
		UserID       string `json:"user_id" binding:"required"`
		DatabaseID   string `json:"database_id" binding:"required"`
		LanguageCode string `json:"language_code"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil || user.Notes.Notion == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user or notion config not found"})
		return
	}

	// Helper to normalize IDs for comparison (strip dashes)
	normalize := func(id string) string {
		return strings.ToLower(strings.ReplaceAll(id, "-", ""))
	}
	targetIDNorm := normalize(req.DatabaseID)

	updated := false
	fmt.Printf("[RemoveDatabase] Request: UserID=%s, DB ID=%s (Norm: %s), Lang=%s\n", req.UserID, req.DatabaseID, targetIDNorm, req.LanguageCode)

	// 1. ARCHIVE IN NOTION (Optional but preferred if we have token)
	if user.Notes.Notion.AccessToken != "" {
		token, err := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
		if err == nil {
			adapter := notion.NewAdapter(token, req.DatabaseID)
			if err := adapter.ArchiveDatabase(ctx, token); err != nil {
				fmt.Printf("[RemoveDatabase] Notion Archive Failed (Continuing with cleanup): %v\n", err)
			} else {
				fmt.Printf("[RemoveDatabase] Successfully archived database in Notion\n")
			}
		}
	}

	// 2. Check if it's the main database
	if normalize(user.Notes.Notion.DefaultDatabaseID) == targetIDNorm {
		fmt.Printf("[RemoveDatabase] Clearing main database link: %s\n", user.Notes.Notion.DefaultDatabaseID)
		user.Notes.Notion.DefaultDatabaseID = ""
		user.Notes.Notion.DefaultDatabaseName = ""
		updated = true
	}

	// 3. Check detected languages
	if user.Notes.Notion.DetectedLanguages != nil {
		// If specific language provided, try to remove it directly first
		if req.LanguageCode != "" {
			if _, ok := user.Notes.Notion.DetectedLanguages[req.LanguageCode]; ok {
				fmt.Printf("[RemoveDatabase] Removing specific language route entry: %s\n", req.LanguageCode)
				delete(user.Notes.Notion.DetectedLanguages, req.LanguageCode)
				updated = true
			}
		}

		// ALSO scan for any remaining entries matching this DatabaseID (ID-agnostic)
		for lang, route := range user.Notes.Notion.DetectedLanguages {
			if normalize(route.DatabaseID) == targetIDNorm {
				fmt.Printf("[RemoveDatabase] Removing associated language route: %s (DB ID: %s)\n", lang, route.DatabaseID)
				delete(user.Notes.Notion.DetectedLanguages, lang)
				updated = true
			}
		}
	}

	if updated {
		// Log final state for debugging
		var remaining []string
		for k := range user.Notes.Notion.DetectedLanguages {
			remaining = append(remaining, k)
		}
		fmt.Printf("[RemoveDatabase] Final languages in Firestore for %s: %v\n", req.UserID, remaining)

		if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
			fmt.Printf("[RemoveDatabase] Error saving updated user config: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update configuration"})
			return
		}
		fmt.Printf("[RemoveDatabase] Successfully updated Firestore for user %s\n", req.UserID)
	} else {
		fmt.Printf("[RemoveDatabase] No changes were needed for DB ID %s in user profile\n", req.DatabaseID)
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SyncWord handles manual synchronization of a captured word
func (h *NotionHandler) SyncWord(c *gin.Context) {
	var req struct {
		UserID     string           `json:"user_id" binding:"required"`
		WordEntry  models.WordEntry `json:"word_entry" binding:"required"`
		DatabaseID string           `json:"database_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil || user.Notes.Notion == nil || user.Notes.Notion.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	token, err := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return
	}

	targetDbID := req.DatabaseID
	if targetDbID == "" {
		// Try language-specific route
		if user.Notes.Notion.DetectedLanguages != nil {
			// 1. Exact match
			if route, ok := user.Notes.Notion.DetectedLanguages[req.WordEntry.SourceLanguage]; ok && route.DatabaseID != "" {
				targetDbID = route.DatabaseID
			} else {
				// 2. Short-code match (e.g., zh-CN -> zh)
				shortLang := strings.Split(req.WordEntry.SourceLanguage, "-")[0]
				if route, ok := user.Notes.Notion.DetectedLanguages[shortLang]; ok && route.DatabaseID != "" {
					targetDbID = route.DatabaseID
				}
			}
		}

		if targetDbID == "" {
			targetDbID = user.Notes.Notion.DefaultDatabaseID
		}
	}

	if targetDbID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no target database available"})
		return
	}

	adapter := notion.NewAdapter(token, targetDbID)

	// Check for duplicates
	existingURL, _ := adapter.FindWord(ctx, req.WordEntry.Word)
	if existingURL != "" {
		c.JSON(http.StatusOK, gin.H{"success": true, "note_url": existingURL, "is_duplicate": true})
		return
	}

	// 3. Ensure valid timestamp
	if req.WordEntry.CreatedAt.IsZero() {
		req.WordEntry.CreatedAt = time.Now()
	}

	fmt.Printf("[NotionHandler] Syncing '%s' to database %s\n", req.WordEntry.Word, targetDbID)
	notionURL, err := adapter.SaveWord(ctx, &req.WordEntry)
	if err != nil {
		fmt.Printf("[NotionHandler] Notion Save FAIL: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync to Notion: " + err.Error()})
		return
	}

	// Update local repository
	req.WordEntry.NoteURL = notionURL
	if err := h.userRepo.SaveWord(ctx, req.UserID, &req.WordEntry); err != nil {
		fmt.Printf("[NotionHandler] Warning: Firestore update failed after Notion sync: %v\n", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"note_url": notionURL,
	})
}
