package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/geekabo93/lingofetch/internal/config"
	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/geekabo93/lingofetch/internal/pkg/crypto"
	"github.com/geekabo93/lingofetch/internal/pkg/languages"
	"github.com/geekabo93/lingofetch/internal/repository"
	"github.com/geekabo93/lingofetch/internal/sync/notion"
	"github.com/geekabo93/lingofetch/internal/sync/obsidian"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	cfg      *config.Config
	userRepo repository.Repository
}

func NewUserHandler(cfg *config.Config, userRepo repository.Repository) *UserHandler {
	return &UserHandler{
		cfg:      cfg,
		userRepo: userRepo,
	}
}

// GetStatus returns the unified status (Notion, Obsidian, AI Prefs)
func (h *UserHandler) GetStatus(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusOK, gin.H{"connected": false})
		return
	}

	user, err := h.userRepo.GetUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"connected": false})
		return
	}

	isNotionConnected := user.Notes != nil && user.Notes.Notion != nil && user.Notes.Notion.AccessToken != ""
	isObsidianConnected := user.Notes != nil && user.Notes.Obsidian != nil && user.Notes.Obsidian.AccessToken != ""

	resp := gin.H{
		"connected": isNotionConnected || isObsidianConnected,
		"user_id":   user.ID,
		"ai_prefs":  user.AIPrefs,
	}

	activeProvider := "notion"
	if user.Notes != nil && user.Notes.ActiveProvider != "" {
		activeProvider = user.Notes.ActiveProvider
	}
	resp["active_note_provider"] = activeProvider

	// Notion Info
	if isNotionConnected {
		notionInfo := gin.H{
			"connected":             true,
			"default_database_id":   user.Notes.Notion.DefaultDatabaseID,
			"default_database_name": user.Notes.Notion.DefaultDatabaseName,
			"workspace":             user.Notes.Notion.WorkspaceName,
		}

		// Enrich languages
		enriched := make(map[string]models.LanguageRoute)
		for code, route := range user.Notes.Notion.DetectedLanguages {
			if route.LanguageName == "" {
				route.LanguageName = languages.GetLanguageName(code)
			}
			enriched[code] = route
		}
		notionInfo["detected_languages"] = enriched
		resp["notion"] = notionInfo
	}

	// Obsidian Info
	if isObsidianConnected {
		obsidianInfo := gin.H{
			"connected":             true,
			"default_database_id":   user.Notes.Obsidian.DefaultDatabaseID,
			"default_database_name": user.Notes.Obsidian.DefaultDatabaseName,
			"base_url":              user.Notes.Obsidian.BaseURL,
		}

		// Enrich languages
		enriched := make(map[string]models.LanguageRoute)
		for code, route := range user.Notes.Obsidian.DetectedLanguages {
			if route.LanguageName == "" {
				route.LanguageName = languages.GetLanguageName(code)
			}
			enriched[code] = route
		}
		obsidianInfo["detected_languages"] = enriched
		resp["obsidian"] = obsidianInfo
	}

	// Legacy/Compatibility: Flatten fields based on active provider or default to Notion
	if activeProvider == "obsidian" && isObsidianConnected {
		resp["database_id"] = user.Notes.Obsidian.DefaultDatabaseID
		resp["database_name"] = user.Notes.Obsidian.DefaultDatabaseName
		if resp["database_name"] == "" {
			resp["database_name"] = models.DefaultObsidianVaultName
		}
		resp["detected_languages"] = resp["obsidian"].(gin.H)["detected_languages"]
	} else if isNotionConnected {
		resp["database_id"] = user.Notes.Notion.DefaultDatabaseID
		resp["database_name"] = user.Notes.Notion.DefaultDatabaseName
		if resp["database_name"] == "" {
			resp["database_name"] = models.DefaultDatabaseName
		}
		resp["detected_languages"] = resp["notion"].(gin.H)["detected_languages"]
	} else if isObsidianConnected {
		resp["database_id"] = user.Notes.Obsidian.DefaultDatabaseID
		resp["database_name"] = user.Notes.Obsidian.DefaultDatabaseName
		if resp["database_name"] == "" {
			resp["database_name"] = models.DefaultObsidianVaultName
		}
		resp["detected_languages"] = resp["obsidian"].(gin.H)["detected_languages"]
	} else {
		resp["detected_languages"] = make(map[string]models.LanguageRoute)
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateSettings handles common settings and provider-specific bypass
func (h *UserHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		UserID             string                          `json:"user_id" binding:"required"`
		AIProvider         string                          `json:"ai_provider"`
		TargetLanguage     string                          `json:"target_language"`
		ActiveNoteProvider string                          `json:"active_note_provider"`
		DatabaseName       string                          `json:"database_name"`
		DetectedLanguages  map[string]models.LanguageRoute `json:"detected_languages"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil {
		user = &models.User{ID: req.UserID, CreatedAt: time.Now()}
	}

	updated := false

	// Note Provider Prefs
	if req.ActiveNoteProvider != "" {
		if user.Notes == nil {
			user.Notes = &models.NotesConfig{}
		}
		if user.Notes.ActiveProvider != req.ActiveNoteProvider {
			user.Notes.ActiveProvider = req.ActiveNoteProvider
			updated = true
		}
	}

	// Global AI Prefs
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

	// Notion Specific (Legacy compatibility)
	if (req.DatabaseName != "" || req.DetectedLanguages != nil) && user.Notes != nil && user.Notes.Notion != nil {
		if req.DatabaseName != "" && user.Notes.Notion.DefaultDatabaseName != req.DatabaseName {
			user.Notes.Notion.DefaultDatabaseName = req.DatabaseName
			user.Notes.Notion.DefaultDatabaseID = "" // Trigger re-discovery
			updated = true
		}
		if req.DetectedLanguages != nil {
			if user.Notes.Notion.DetectedLanguages == nil {
				user.Notes.Notion.DetectedLanguages = req.DetectedLanguages
			} else {
				for k, v := range req.DetectedLanguages {
					user.Notes.Notion.DetectedLanguages[k] = v
				}
			}
			updated = true
		}
	}

	if updated {
		if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DisconnectProvider handles disconnecting from any note provider
func (h *UserHandler) DisconnectProvider(c *gin.Context) {
	var req struct {
		UserID   string `json:"user_id"`
		Provider string `json:"provider"` // Optional: "notion" or "obsidian"
	}

	// Try body first, then query
	if err := c.ShouldBindJSON(&req); err != nil {
		req.UserID = c.Query("user_id")
		req.Provider = c.Query("provider")
	}

	if req.UserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if user.Notes == nil {
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}

	target := req.Provider
	if target == "" {
		target = user.Notes.ActiveProvider
	}
	if target == "" {
		target = "notion" // Default
	}

	if target == "notion" && user.Notes.Notion != nil {
		user.Notes.Notion.AccessToken = ""
	} else if target == "obsidian" && user.Notes.Obsidian != nil {
		user.Notes.Obsidian.AccessToken = ""
	}

	if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to disconnect"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListDatabases returns all available databases/vaults for the active provider
func (h *UserHandler) ListDatabases(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	activeProvider := "notion"
	if user.Notes != nil && user.Notes.ActiveProvider != "" {
		activeProvider = user.Notes.ActiveProvider
	}

	if activeProvider == "obsidian" {
		if user.Notes == nil || user.Notes.Obsidian == nil || user.Notes.Obsidian.AccessToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Obsidian not connected"})
			return
		}
		// Obsidian doesn't have "databases" in the same way, return the vault reference
		c.JSON(http.StatusOK, gin.H{"databases": []gin.H{
			{"id": "obsidian-vault", "name": models.DefaultObsidianVaultName},
		}})
		return
	}

	// Notion logic
	if user.Notes == nil || user.Notes.Notion == nil || user.Notes.Notion.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	token, _ := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
	adapter := notion.NewAdapter(token, "")
	databases, err := adapter.ListDatabases(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list Notion databases: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"databases": databases})
}

// CreateDatabase handles creating a new language database/file for the active provider
func (h *UserHandler) CreateDatabase(c *gin.Context) {
	var req struct {
		UserID       string            `json:"user_id" binding:"required"`
		LanguageCode string            `json:"language_code" binding:"required"`
		DatabaseName string            `json:"database_name" binding:"required"`
		WordEntry    *models.WordEntry `json:"word_entry"` // Optional: sync immediately
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	activeProvider := "notion"
	if user.Notes != nil && user.Notes.ActiveProvider != "" {
		activeProvider = user.Notes.ActiveProvider
	}

	if activeProvider == "obsidian" {
		h.handleObsidianCreate(c, user, req.LanguageCode, req.DatabaseName, req.WordEntry)
	} else {
		h.handleNotionCreate(c, user, req.LanguageCode, req.DatabaseName, req.WordEntry)
	}
}

func (h *UserHandler) handleNotionCreate(c *gin.Context, user *models.User, langCode, dbName string, word *models.WordEntry) {
	if user.Notes == nil || user.Notes.Notion == nil || user.Notes.Notion.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	token, _ := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
	adapter := notion.NewAdapter(token, "")
	pageID := user.Notes.Notion.ParentPageID

	ctx := c.Request.Context()
	if pageID == "" {
		_, discoveredPageID, err := adapter.DiscoverDatabaseID(ctx, models.DefaultDatabaseName)
		if err == nil && discoveredPageID != "" {
			pageID = discoveredPageID
			user.Notes.Notion.ParentPageID = pageID
		}
	}

	if pageID == "" {
		c.JSON(http.StatusFailedDependency, gin.H{"error": "Could not find a parent page in Notion."})
		return
	}

	newDbID, err := adapter.CreateDatabase(ctx, pageID, dbName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create Notion database: " + err.Error()})
		return
	}

	if user.Notes.Notion.DetectedLanguages == nil {
		user.Notes.Notion.DetectedLanguages = make(map[string]models.LanguageRoute)
	}
	user.Notes.Notion.DetectedLanguages[langCode] = models.LanguageRoute{
		DatabaseID:   newDbID,
		DatabaseName: dbName,
		LanguageName: languages.GetLanguageName(langCode),
	}

	resp := gin.H{"success": true, "database_id": newDbID}

	// Immediate Sync if word provided
	if word != nil {
		if word.CreatedAt.IsZero() {
			word.CreatedAt = time.Now()
		}
		syncAdapter := notion.NewAdapter(token, newDbID)
		url, err := syncAdapter.SaveWord(ctx, word)
		if err == nil {
			word.NoteURL = url
			_ = h.userRepo.SaveWord(ctx, user.ID, word)
			resp["note_url"] = url
		}
	}

	h.userRepo.CreateOrUpdateUser(ctx, user)
	c.JSON(http.StatusOK, resp)
}

func (h *UserHandler) handleObsidianCreate(c *gin.Context, user *models.User, langCode, dbName string, word *models.WordEntry) {
	if user.Notes == nil || user.Notes.Obsidian == nil || user.Notes.Obsidian.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Obsidian not connected"})
		return
	}

	token, _ := crypto.Decrypt(user.Notes.Obsidian.AccessToken, h.cfg.EncryptionKey)
	basePath := fmt.Sprintf("LingoFetch/%s/%s.base", dbName, dbName)
	adapter := obsidian.NewAdapter(user.Notes.Obsidian.BaseURL, token, basePath)

	ctx := c.Request.Context()
	id, err := adapter.CreateDatabase(ctx, basePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create Obsidian base: " + err.Error()})
		return
	}

	if user.Notes.Obsidian.DetectedLanguages == nil {
		user.Notes.Obsidian.DetectedLanguages = make(map[string]models.LanguageRoute)
	}
	user.Notes.Obsidian.DetectedLanguages[langCode] = models.LanguageRoute{
		DatabaseID:   id,
		DatabaseName: dbName,
		LanguageName: languages.GetLanguageName(langCode),
	}

	resp := gin.H{"success": true, "database_id": id}

	// Immediate Sync if word provided
	if word != nil {
		if word.CreatedAt.IsZero() {
			word.CreatedAt = time.Now()
		}
		url, err := adapter.SaveWord(ctx, word)
		if err == nil {
			word.NoteURL = url
			_ = h.userRepo.SaveWord(ctx, user.ID, word)
			resp["note_url"] = url
		}
	}

	h.userRepo.CreateOrUpdateUser(ctx, user)
	c.JSON(http.StatusOK, resp)
}

// ProxyQuery handles database queries from the extension, routing to the active provider
func (h *UserHandler) ProxyQuery(c *gin.Context) {
	databaseID := c.Param("id")
	// Remove leading slash if using wildcard route
	databaseID = strings.TrimPrefix(databaseID, "/")
	// Remove /query suffix if it came through the wildcard
	databaseID = strings.TrimSuffix(databaseID, "/query")

	userID := c.Query("user_id")
	var bodyMap map[string]interface{}
	if err := c.ShouldBindJSON(&bodyMap); err == nil {
		if userID == "" {
			if bid, ok := bodyMap["user_id"].(string); ok {
				userID = bid
			}
		}
		delete(bodyMap, "user_id")
	}

	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	ctx := c.Request.Context()
	user, _ := h.userRepo.GetUser(ctx, userID)
	activeProvider := "notion"
	if user != nil && user.Notes != nil && user.Notes.ActiveProvider != "" {
		activeProvider = user.Notes.ActiveProvider
	}

	if activeProvider == "obsidian" {
		// For Obsidian, we just return "No results" to allow the extension to proceed with capture
		// since Obsidian doesn't support complex Notion queries.
		c.JSON(http.StatusOK, gin.H{"results": []interface{}{}})
		return
	}

	// Notion Proxying
	if user == nil || user.Notes.Notion == nil || user.Notes.Notion.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	token, _ := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
	jsonBytes, _ := json.Marshal(bodyMap)

	apiURL := fmt.Sprintf("https://api.notion.com/v1/databases/%s/query", databaseID)
	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonBytes))
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Notion-Version", "2022-06-28")
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	resBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", resBody)
}

// ProxyCreate handles entry creation from the extension, routing to the active provider
func (h *UserHandler) ProxyCreate(c *gin.Context) {
	userID := c.Query("user_id")
	var bodyMap map[string]interface{}
	if err := c.ShouldBindJSON(&bodyMap); err == nil {
		if userID == "" {
			if bid, ok := bodyMap["user_id"].(string); ok {
				userID = bid
			}
		}
		delete(bodyMap, "user_id")
	}

	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	ctx := c.Request.Context()
	user, _ := h.userRepo.GetUser(ctx, userID)
	activeProvider := "notion"
	if user != nil && user.Notes != nil && user.Notes.ActiveProvider != "" {
		activeProvider = user.Notes.ActiveProvider
	}

	if activeProvider == "obsidian" {
		// Attempt to parse the Notion-style payload into a WordEntry for Obsidian
		// This is a "Best Effort" conversion
		wordEntry := &models.WordEntry{
			CreatedAt: time.Now(),
		}

		// Robust parsing of Notion properties to WordEntry
		if props, ok := bodyMap["properties"].(map[string]interface{}); ok {
			// Helper to extract text from rich_text or title fields
			getString := func(key string) string {
				field, ok := props[key].(map[string]interface{})
				if !ok {
					return ""
				}
				// Title field
				if list, ok := field["title"].([]interface{}); ok && len(list) > 0 {
					if text, ok := list[0].(map[string]interface{})["text"].(map[string]interface{}); ok {
						return text["content"].(string)
					}
				}
				// Rich Text field
				if list, ok := field["rich_text"].([]interface{}); ok && len(list) > 0 {
					if text, ok := list[0].(map[string]interface{})["text"].(map[string]interface{}); ok {
						return text["content"].(string)
					}
				}
				// Select field (usually for Part of Speech)
				if sel, ok := field["select"].(map[string]interface{}); ok {
					return sel["name"].(string)
				}
				return ""
			}

			wordEntry.Word = getString("Word")
			wordEntry.Definition = getString("Definition")
			wordEntry.Pronunciation = getString("Pronunciation")
			wordEntry.PartOfSpeech = getString("Part of Speech")
			wordEntry.Example = getString("Example")
			wordEntry.SourceLanguage = getString("Source Language")
		}

		if wordEntry.Word == "" {
			c.JSON(http.StatusOK, gin.H{"success": true})
			return
		}

		// Handle Obsidian Save
		token, _ := crypto.Decrypt(user.Notes.Obsidian.AccessToken, h.cfg.EncryptionKey)
		// We need a path. Try to get it from the body or use default
		targetPath := fmt.Sprintf("LingoFetch/%s/%s.base", models.DefaultDatabaseName, models.DefaultDatabaseName)
		if parent, ok := bodyMap["parent"].(map[string]interface{}); ok {
			if dbID, ok := parent["database_id"].(string); ok {
				targetPath = dbID
			}
		}

		adapter := obsidian.NewAdapter(user.Notes.Obsidian.BaseURL, token, targetPath)
		url, err := adapter.SaveWord(ctx, wordEntry)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "url": url})
		return
	}

	// Notion Proxying
	if user == nil || user.Notes.Notion == nil || user.Notes.Notion.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	token, _ := crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
	jsonBytes, _ := json.Marshal(bodyMap)

	apiURL := "https://api.notion.com/v1/pages"
	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonBytes))
	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Notion-Version", "2022-06-28")
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	resBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", resBody)
}
