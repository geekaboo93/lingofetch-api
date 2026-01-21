package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/geekabo93/lingofetch/internal/ai"
	"github.com/geekabo93/lingofetch/internal/config"
	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/geekabo93/lingofetch/internal/pkg/crypto"
	"github.com/geekabo93/lingofetch/internal/pkg/languages"
	"github.com/geekabo93/lingofetch/internal/pkg/resources"
	"github.com/geekabo93/lingofetch/internal/repository"
	"github.com/geekabo93/lingofetch/internal/sync"
	"github.com/geekabo93/lingofetch/internal/sync/notion"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// CaptureHandler handles word capture requests
type CaptureHandler struct {
	cfg          *config.Config
	providers    map[ai.ProviderType]ai.Provider
	defaultType  ai.ProviderType
	userRepo     repository.Repository
	noteProvider sync.NoteProvider // Fallback provider (internal)
}

// NewCaptureHandler creates a new capture handler
func NewCaptureHandler(
	cfg *config.Config,
	providers map[ai.ProviderType]ai.Provider,
	defaultType ai.ProviderType,
	userRepo repository.Repository,
	noteProvider sync.NoteProvider,
) *CaptureHandler {
	return &CaptureHandler{
		cfg:          cfg,
		providers:    providers,
		defaultType:  defaultType,
		userRepo:     userRepo,
		noteProvider: noteProvider,
	}
}

// Handle processes the word capture request with concurrent AI and Notion operations
func (h *CaptureHandler) Handle(c *gin.Context) {
	fmt.Println("[Trace] Handle: Entry point reached")
	var req models.CaptureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[Trace] Handle: JSON Bind Error: %v\n", err)
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Normalize word
	req.Word = strings.TrimSpace(strings.ToLower(req.Word))
	fmt.Printf("[Trace] Starting capture for word: %s (User: %s)\n", req.Word, req.UserID)

	// Create context with timeout for external API calls
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	// 1. RESOLVE USER CONFIG & NOTION TOKEN
	var user *models.User
	var decryptedToken string
	if req.UserID != "" && h.userRepo != nil {
		u, err := h.userRepo.GetUser(ctx, req.UserID)
		if err == nil {
			user = u
			if user.NotionConfig != nil && user.NotionConfig.AccessToken != "" {
				decryptedToken, _ = crypto.Decrypt(user.NotionConfig.AccessToken, h.cfg.EncryptionKey)
			}
		}
	}

	// 2. Determine which AI provider to use
	pType := h.defaultType
	// Order of priority: Request > User Preferences > App Default
	providerSource := req.Provider
	if providerSource == "" && user != nil && user.AIPrefs != nil {
		providerSource = user.AIPrefs.DefaultProvider
	}

	if providerSource != "" {
		if pt, err := ai.GetProviderFromString(providerSource); err == nil {
			pType = pt
		}
	}

	// 2.5. Check if user provided custom API key
	var aiProvider ai.Provider
	if req.CustomAPIKey != "" {
		// User provided their own API key - create a new provider instance
		fmt.Printf("[Trace] [Capture] Using custom API key for provider: %s\n", pType)
		customProvider, err := ai.NewProvider(ctx, ai.ProviderConfig{
			Type:   pType,
			APIKey: req.CustomAPIKey,
		})
		if err != nil {
			fmt.Printf("[Trace] [Capture] Failed to create custom provider: %v\n", err)
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Success: false,
				Error:   fmt.Sprintf("Invalid custom API key for %s: %v", pType, err),
			})
			return
		}
		aiProvider = customProvider
	} else {
		// Use backend's default providers
		var ok bool
		aiProvider, ok = h.providers[pType]
		if !ok {
			aiProvider, _ = h.providers[h.defaultType]
		}
	}

	// 3. Determine Target Language
	// Order of priority: Request > User Preferences > App Default
	targetLang := req.TargetLanguage
	if targetLang == "" && user != nil && user.AIPrefs != nil {
		targetLang = user.AIPrefs.TargetLanguage
	}
	if targetLang == "" {
		targetLang = "English (US)"
	}

	// 4. CHECK IF WORD ALREADY EXISTS (Local Repository - FAST)
	fmt.Printf("[Trace] [Capture] RECEIVED request for word: %s (User: %s)\n", req.Word, req.UserID)
	if req.UserID != "" && h.userRepo != nil {
		existing, err := h.userRepo.FindWord(ctx, req.UserID, req.Word, targetLang)
		if err == nil && existing != nil {
			msg := resources.GetRandomMessage("mastery")
			fmt.Printf("[Trace] [Capture] Word '%s' already exists in local repo. Skipping AI.\n", req.Word)

			c.JSON(http.StatusOK, models.CaptureResponse{
				Success:       true,
				Word:          existing.Word,
				Definition:    existing.Definition,
				Pronunciation: existing.Pronunciation,
				PartOfSpeech:  existing.PartOfSpeech,
				Example:       existing.Example,
				NotionURL:     existing.NotionURL,
				Message:       msg,
				IsDuplicate:   true,
				Timestamp:     existing.CreatedAt,
			})
			return
		}
	}

	// 5. START AI GENERATION
	fmt.Printf("[Trace] [Capture] Calling AI (%v) for '%s' (Target: %s)...\n", aiProvider.Name(), req.Word, targetLang)
	definition, err := aiProvider.GenerateDefinition(ctx, req.Word, targetLang, req.Context, req.PageLanguage, req.SourceLanguage)
	if err != nil {
		fmt.Printf("[Trace] [Capture] Primary AI fail: %v. Fallback enabled.\n", err)
		// Only fallback if not using custom API key
		if req.CustomAPIKey == "" && pType != h.defaultType {
			if fallbackProvider := h.providers[h.defaultType]; fallbackProvider != nil {
				definition, err = fallbackProvider.GenerateDefinition(ctx, req.Word, targetLang, req.Context, req.PageLanguage, req.SourceLanguage)
			}
		}
	}

	if err != nil {
		fmt.Printf("[Trace] [Capture] AI ERROR: %v\n", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Success: false, Error: err.Error()})
		return
	}
	fmt.Printf("[Trace] [Capture] AI Success for '%s' (Source Lang: %s)\n", req.Word, definition.LanguageCode)

	// 2.5 HANDLE AMBIGUITY
	if definition.IsAmbiguous && len(definition.PossibleLanguages) > 1 {
		fmt.Printf("[Handle] Word '%s' is ambiguous. Possible languages: %v\n", req.Word, definition.PossibleLanguages)
		c.JSON(http.StatusOK, models.CaptureResponse{
			Success:           true,
			Word:              definition.Word,
			IsAmbiguous:       true,
			PossibleLanguages: definition.PossibleLanguages,
		})
		return
	}

	// Auto-resolve if AI marked as ambiguous but only gave 1 option
	if definition.IsAmbiguous && len(definition.PossibleLanguages) == 1 {
		fmt.Printf("[Handle] Auto-resolving ambiguity for '%s' to: %s\n", req.Word, definition.PossibleLanguages[0])
		definition.IsAmbiguous = false
		definition.LanguageCode = definition.PossibleLanguages[0]
	}

	// 3. RESOLVE TARGET DATABASE
	targetDbID, suggestRouting := h.resolveTargetDatabase(ctx, user, definition.LanguageCode)
	sourceLang := definition.LanguageCode

	// 4. ENSURE DATABASE EXISTS & CHECK FOR DUPLICATES
	var targetNoteProvider sync.NoteProvider = h.noteProvider
	var existingURL string

	if decryptedToken != "" {
		// If the resolved ID is missing or known to be broken, the repair logic is here
		if targetDbID == "" || (user != nil && user.NotionConfig != nil && targetDbID == user.NotionConfig.DatabaseID) {
			// Validate existence and repair if needed
			validID, err := h.ensureDatabaseValid(ctx, user, decryptedToken, targetDbID)
			if err == nil {
				targetDbID = validID
			}
		}

		if targetDbID != "" {
			fmt.Printf("[Trace] [Capture] Checking Notion for duplicate in DB: %s\n", targetDbID)
			adapter := notion.NewAdapter(decryptedToken, targetDbID)
			targetNoteProvider = adapter
			existingURL, _ = adapter.FindWord(ctx, definition.Word)
		}
	}
	// 5. RESPOND
	fmt.Printf("[Trace] [Capture] Sending response for '%s' (HoldSync: %v)\n", req.Word, suggestRouting)
	if existingURL != "" {
		msg := resources.GetRandomMessage("mastery")
		c.JSON(http.StatusOK, models.CaptureResponse{
			Success:        true,
			Word:           definition.Word,
			Definition:     definition.Definition,
			Pronunciation:  definition.Pronunciation,
			PartOfSpeech:   definition.PartOfSpeech,
			Example:        definition.Example,
			NotionURL:      existingURL,
			Message:        msg,
			IsDuplicate:    true,
			SourceLanguage: sourceLang,
			Timestamp:      time.Now(),
		})
		return
	}

	// 5. RESPOND
	holdSync := false
	if suggestRouting && decryptedToken != "" {
		holdSync = true
	}

	c.JSON(http.StatusOK, models.CaptureResponse{
		Success:        true,
		Word:           definition.Word,
		Definition:     definition.Definition,
		Pronunciation:  definition.Pronunciation,
		PartOfSpeech:   definition.PartOfSpeech,
		Example:        definition.Example,
		SourceLanguage: sourceLang,
		SuggestRouting: suggestRouting,
		HoldSync:       holdSync,
		Timestamp:      time.Now(),
	})

	// 6. SYNC IN BACKGROUND (Only if not holding for routing)
	if holdSync {
		fmt.Printf("[Capture] Holding sync for new language: %s\n", sourceLang)
		return
	}

	go func() {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer bgCancel()

		// Update detected languages for the user
		if user != nil && sourceLang != "" {
			if user.NotionConfig.DetectedLanguages == nil {
				user.NotionConfig.DetectedLanguages = make(map[string]models.LanguageRoute)
			}
			if _, exists := user.NotionConfig.DetectedLanguages[sourceLang]; !exists {
				user.NotionConfig.DetectedLanguages[sourceLang] = models.LanguageRoute{
					LanguageName: languages.GetLanguageName(sourceLang),
				}
				_ = h.userRepo.CreateOrUpdateUser(bgCtx, user)
			}
		}

		createdAt := time.Now()
		if req.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, req.Timestamp); err == nil {
				createdAt = t
			} else if t, err := time.Parse("2006-01-02T15:04:05.000Z", req.Timestamp); err == nil {
				createdAt = t
			}
		}

		wordEntry := &models.WordEntry{
			ID:             uuid.New().String(),
			Word:           definition.Word,
			Definition:     definition.Definition,
			Pronunciation:  definition.Pronunciation,
			PartOfSpeech:   definition.PartOfSpeech,
			Example:        definition.Example,
			TargetLanguage: targetLang,
			SourceLanguage: sourceLang,
			CreatedAt:      createdAt,
		}

		fmt.Printf("[Background] Syncing %s to Notion/Firestore...\n", wordEntry.Word)

		// Run repo save and notion sync in parallel
		gBg, gBgCtx := errgroup.WithContext(bgCtx)

		gBg.Go(func() error {
			if req.UserID != "" && h.userRepo != nil {
				return h.userRepo.SaveWord(gBgCtx, req.UserID, wordEntry)
			}
			return nil
		})

		gBg.Go(func() error {
			url, err := targetNoteProvider.SaveWord(gBgCtx, wordEntry)
			if err == nil {
				wordEntry.NotionURL = url
			}
			return err
		})

		if err := gBg.Wait(); err != nil {
			fmt.Printf("[Background] ERROR syncing %s: %v\n", wordEntry.Word, err)
		} else {
			fmt.Printf("[Background] Successfully synced %s to Notion and User Repository. URL: %s\n", wordEntry.Word, wordEntry.NotionURL)
		}
	}()
}

// HealthHandler handles health check requests
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

func (h *HealthHandler) Handle(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "lingofetch-api",
		"time":    time.Now().UTC(),
	})
}

// GetStatus checks if the user is connected to Notion
func (h *CaptureHandler) GetStatus(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" || h.userRepo == nil {
		c.JSON(http.StatusOK, gin.H{
			"connected": false,
		})
		return
	}
	user, err := h.userRepo.GetUser(c.Request.Context(), userID)
	if err != nil {
		fmt.Printf("[GetStatus] Error fetching user %s: %v\n", userID, err)
		c.JSON(http.StatusOK, gin.H{
			"connected": false,
		})
		return
	}

	resp := gin.H{
		"connected":          user.NotionConfig != nil && user.NotionConfig.AccessToken != "",
		"workspace_name":     "",
		"workspace_icon":     "",
		"database_id":        "",
		"database_name":      "",
		"detected_languages": make(map[string]models.LanguageRoute),
		"ai_preferences":     make(map[string]string),
	}

	if user.AIPrefs != nil {
		resp["ai_preferences"] = gin.H{
			"default_provider": user.AIPrefs.DefaultProvider,
			"target_language":  user.AIPrefs.TargetLanguage,
		}
	}

	if user.NotionConfig != nil {
		resp["workspace_name"] = user.NotionConfig.WorkspaceName
		resp["workspace_icon"] = user.NotionConfig.WorkspaceIcon
		resp["database_id"] = user.NotionConfig.DatabaseID
		resp["database_name"] = user.NotionConfig.DatabaseName

		// Enrich with language names
		enriched := make(map[string]models.LanguageRoute)
		for code, route := range user.NotionConfig.DetectedLanguages {
			if route.LanguageName == "" {
				route.LanguageName = languages.GetLanguageName(code)
			}
			enriched[code] = route
		}
		resp["detected_languages"] = enriched
	}

	fmt.Printf("[GetStatus] User: %s, Connected: %v, DB: %s\n", userID, resp["connected"], resp["database_id"])
	c.JSON(http.StatusOK, resp)
}

// UpdateSettings updates user preferences or Notion configuration
func (h *CaptureHandler) UpdateSettings(c *gin.Context) {
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
		fmt.Printf("[UpdateSettings] User %s not found. Creating new record for settings.\n", req.UserID)
		user = &models.User{
			ID:        req.UserID,
			CreatedAt: time.Now(),
		}
	}

	updated := false
	if req.DatabaseName != "" {
		if user.NotionConfig == nil {
			user.NotionConfig = &models.NotionConfig{}
		}
		if user.NotionConfig.DatabaseName != req.DatabaseName {
			user.NotionConfig.DatabaseName = req.DatabaseName
			// Clear DatabaseID if the name changed, to force a re-discovery on next capture
			user.NotionConfig.DatabaseID = ""
			updated = true
			fmt.Printf("[CaptureHandler] Updated DatabaseName for %s to '%s' (Triggering re-discovery)\n", req.UserID, req.DatabaseName)
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
		if user.NotionConfig == nil {
			user.NotionConfig = &models.NotionConfig{}
		}
		// Merge or replace
		if user.NotionConfig.DetectedLanguages == nil {
			user.NotionConfig.DetectedLanguages = req.DetectedLanguages
		} else {
			for k, v := range req.DetectedLanguages {
				user.NotionConfig.DetectedLanguages[k] = v
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
func (h *CaptureHandler) ListDatabases(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" || h.userRepo == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	user, err := h.userRepo.GetUser(c.Request.Context(), userID)
	if err != nil || user.NotionConfig == nil || user.NotionConfig.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "notion not connected"})
		return
	}

	decryptedToken, err := crypto.Decrypt(user.NotionConfig.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return
	}

	adapter := notion.NewAdapter(decryptedToken, "")
	databases, err := adapter.ListDatabases(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"databases": databases,
	})
}

// CreateLanguageDatabase creates a new Notion database for a specific language
func (h *CaptureHandler) CreateLanguageDatabase(c *gin.Context) {
	var req struct {
		UserID       string `json:"user_id" binding:"required"`
		LanguageCode string `json:"language_code" binding:"required"`
		DatabaseName string `json:"database_name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil || user.NotionConfig == nil || user.NotionConfig.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "notion not connected"})
		return
	}

	// 🛠️ VALIDATION: Length and Uniqueness
	if len(req.DatabaseName) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database name must be 100 characters or less"})
		return
	}

	trimmedNewName := strings.TrimSpace(req.DatabaseName)
	if strings.EqualFold(trimmedNewName, user.NotionConfig.DatabaseName) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "database name already used by your main dictionary"})
		return
	}
	for _, route := range user.NotionConfig.DetectedLanguages {
		if strings.EqualFold(trimmedNewName, route.DatabaseName) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "database name already used by another language dictionary"})
			return
		}
	}

	decryptedToken, err := crypto.Decrypt(user.NotionConfig.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return
	}

	adapter := notion.NewAdapter(decryptedToken, "")

	// 1. Check if we already have a parent page ID stored for this user
	pageID := user.NotionConfig.ParentPageID

	// 2. If not, try to discover one (and reuse existing database if found)
	if pageID == "" {
		fmt.Printf("[CreateLanguageDatabase] No ParentPageID stored, searching...\n")
		existingDbID, discoveredPageID, err := adapter.DiscoverDatabaseID(ctx, req.DatabaseName)
		if err == nil && existingDbID != "" {
			fmt.Printf("[CreateLanguageDatabase] Found existing database '%s' (ID: %s). Reusing.\n", req.DatabaseName, existingDbID)
			updateUserAndRespond(c, h, user, req.LanguageCode, existingDbID, req.DatabaseName, discoveredPageID)
			return
		}
		pageID = discoveredPageID
	}

	// 3. Last resort discovery if still no pageID
	if pageID == "" {
		_, pageID, _ = adapter.DiscoverDatabaseID(ctx, "NON_EXISTENT_QUERY_FORCELIST_PAGES")
	}

	if pageID == "" {
		c.JSON(http.StatusFailedDependency, gin.H{"error": "could not find a parent page in Notion. Please ensure you have shared at least one page (not just a database) with the integration."})
		return
	}

	// 4. Create the new database
	newDbID, err := adapter.CreateDatabase(ctx, pageID, req.DatabaseName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create database: " + err.Error()})
		return
	}

	updateUserAndRespond(c, h, user, req.LanguageCode, newDbID, req.DatabaseName, pageID)
}

func updateUserAndRespond(c *gin.Context, h *CaptureHandler, user *models.User, langCode, dbID, dbName, parentPageID string) {
	ctx := c.Request.Context()
	if user.NotionConfig.DetectedLanguages == nil {
		user.NotionConfig.DetectedLanguages = make(map[string]models.LanguageRoute)
	}
	user.NotionConfig.DetectedLanguages[langCode] = models.LanguageRoute{
		DatabaseID:   dbID,
		DatabaseName: dbName,
		LanguageName: languages.GetLanguageName(langCode),
	}

	// Ensure we save the parentPageID for future siblings
	if user.NotionConfig.ParentPageID == "" && parentPageID != "" {
		user.NotionConfig.ParentPageID = parentPageID
	}

	if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"database_id":   dbID,
		"database_name": dbName,
	})
}

// RenameDatabase handles renaming an existing Notion database
func (h *CaptureHandler) RenameDatabase(c *gin.Context) {
	var req struct {
		UserID       string `json:"user_id" binding:"required"`
		DatabaseID   string `json:"database_id" binding:"required"`
		NewName      string `json:"new_name" binding:"required"`
		LanguageCode string `json:"language_code"` // Optional: if provided, update the route
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

	if user.NotionConfig == nil || user.NotionConfig.AccessToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Notion not connected"})
		return
	}

	decryptedToken, err := crypto.Decrypt(user.NotionConfig.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return
	}

	// 1. Rename in Notion
	adapter := notion.NewAdapter(decryptedToken, req.DatabaseID)
	if err := adapter.RenameDatabase(ctx, req.NewName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename in Notion: " + err.Error()})
		return
	}

	// 2. Update local config
	updated := false
	if req.DatabaseID == user.NotionConfig.DatabaseID {
		user.NotionConfig.DatabaseName = req.NewName
		updated = true
	}

	// Check language routes
	if user.NotionConfig.DetectedLanguages != nil {
		if req.LanguageCode != "" {
			if route, ok := user.NotionConfig.DetectedLanguages[req.LanguageCode]; ok && route.DatabaseID == req.DatabaseID {
				route.DatabaseName = req.NewName
				user.NotionConfig.DetectedLanguages[req.LanguageCode] = route
				updated = true
			}
		} else {
			// If no lang code provided, scan all routes for this DB ID
			for code, route := range user.NotionConfig.DetectedLanguages {
				if route.DatabaseID == req.DatabaseID {
					route.DatabaseName = req.NewName
					user.NotionConfig.DetectedLanguages[code] = route
					updated = true
				}
			}
		}
	}

	if updated {
		if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
			fmt.Printf("[RenameDatabase] Error saving updated user config: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"name":    req.NewName,
	})
}

// SyncWord handles manual synchronization of a captured word
func (h *CaptureHandler) SyncWord(c *gin.Context) {
	fmt.Println("[Trace] SyncWord: Entry point reached")
	var req struct {
		UserID     string           `json:"user_id" binding:"required"`
		WordEntry  models.WordEntry `json:"word_entry" binding:"required"`
		DatabaseID string           `json:"database_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[Trace] SyncWord: JSON Bind Error: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil {
		fmt.Printf("[Trace] SyncWord: User %s not found: %v\n", req.UserID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// 1. Resolve Database ID
	targetDbID := req.DatabaseID
	if targetDbID == "" {
		targetDbID, _ = h.resolveTargetDatabase(ctx, user, req.WordEntry.SourceLanguage)
	}

	fmt.Printf("[Trace] SyncWord: Targeting DB %s for word %s (User: %s)\n", targetDbID, req.WordEntry.Word, req.UserID)

	if targetDbID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no target database available. Please create one or connect Notion."})
		return
	}

	if user.NotionConfig == nil || user.NotionConfig.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Notion not connected"})
		return
	}

	decryptedToken, err := crypto.Decrypt(user.NotionConfig.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt token"})
		return
	}

	// 2. Defensive check for ID and Timestamp
	if req.WordEntry.ID == "" {
		req.WordEntry.ID = uuid.New().String()
	}
	if req.WordEntry.CreatedAt.IsZero() {
		req.WordEntry.CreatedAt = time.Now()
	}

	// 3. Perform Sync
	if targetDbID != "" {
		newID, err := h.ensureDatabaseValid(ctx, user, decryptedToken, targetDbID)
		if err == nil {
			targetDbID = newID
		}
	}

	adapter := notion.NewAdapter(decryptedToken, targetDbID)

	// Check for duplicates in the specific target database
	existingURL, err := adapter.FindWord(ctx, req.WordEntry.Word)
	if err == nil && existingURL != "" {
		fmt.Printf("[Trace] SyncWord: Word %s already exists in Notion. Skipping.\n", req.WordEntry.Word)
		c.JSON(http.StatusOK, gin.H{"success": true, "notion_url": existingURL, "is_duplicate": true})
		return
	}

	fmt.Printf("[Trace] SyncWord: Saving %s to Notion ID %s...\n", req.WordEntry.Word, targetDbID)
	notionURL, err := adapter.SaveWord(ctx, &req.WordEntry)
	if err != nil {
		fmt.Printf("[Trace] SyncWord: Notion API FAIL: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync to Notion: " + err.Error()})
		return
	}

	// 4. Update local repository
	req.WordEntry.NotionURL = notionURL
	if err := h.userRepo.SaveWord(ctx, req.UserID, &req.WordEntry); err != nil {
		fmt.Printf("[Trace] SyncWord: Firestore SAVE FAIL: %v\n", err)
		// We don't fail the request here because Notion succeeded, but we log the error
	}

	fmt.Printf("[Trace] SyncWord: SUCCESS for %s (URL: %s)\n", req.WordEntry.Word, notionURL)
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"notion_url": notionURL,
	})
}

// DisconnectNotion clears the user's Notion configuration
func (h *CaptureHandler) DisconnectNotion(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" || h.userRepo == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	user.NotionConfig = nil
	if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to disconnect notion"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// resolveTargetDatabase determines the best database ID for a given language
func (h *CaptureHandler) resolveTargetDatabase(ctx context.Context, user *models.User, sourceLang string) (string, bool) {
	if user == nil || user.NotionConfig == nil {
		return "", false
	}

	targetDbID := ""
	suggestRouting := false

	// Normalize sourceLang (e.g., "zh-CN" -> "zh") for matching
	shortLang := strings.Split(sourceLang, "-")[0]

	// Try language-specific route
	if user.NotionConfig.DetectedLanguages != nil {
		// Exact match first
		if route, ok := user.NotionConfig.DetectedLanguages[sourceLang]; ok && route.DatabaseID != "" {
			targetDbID = route.DatabaseID
		} else if route, ok := user.NotionConfig.DetectedLanguages[shortLang]; ok && route.DatabaseID != "" {
			targetDbID = route.DatabaseID
		}
	}

	// Fallback to default
	if targetDbID == "" {
		targetDbID = user.NotionConfig.DatabaseID
		if sourceLang != "" && shortLang != "en" { // Suggest routing for non-english
			suggestRouting = true
		}
	}

	return targetDbID, suggestRouting
}

// ensureDatabaseValid checks if a database ID is still valid in Notion and repairs it if not
func (h *CaptureHandler) ensureDatabaseValid(ctx context.Context, user *models.User, token string, currentID string) (string, error) {
	if user == nil || user.NotionConfig == nil {
		return currentID, nil
	}

	if currentID == "" && user.NotionConfig.DatabaseID == "" {
		// Discovery logic for missing root database
		fmt.Printf("[Trace] Database ID missing, starting discovery for: %s\n", user.NotionConfig.DatabaseName)
		tempAdapter := notion.NewAdapter(token, "")
		targetName := user.NotionConfig.DatabaseName
		if targetName == "" {
			targetName = "LingoFetch Dictionary"
		}
		discID, pageID, _ := tempAdapter.DiscoverDatabaseID(ctx, targetName)
		if discID != "" {
			user.NotionConfig.DatabaseID = discID
			user.NotionConfig.DatabaseName = targetName // Ensure name is synced
			_ = h.userRepo.CreateOrUpdateUser(ctx, user)
			return discID, nil
		} else if pageID != "" {
			if newDbID, err := tempAdapter.CreateDatabase(ctx, pageID, targetName); err == nil {
				user.NotionConfig.DatabaseID = newDbID
				user.NotionConfig.DatabaseName = targetName
				_ = h.userRepo.CreateOrUpdateUser(ctx, user)
				return newDbID, nil
			}
		}
		return "", fmt.Errorf("could not discover or create database")
	}

	// If ID exists, validate it
	adapter := notion.NewAdapter(token, currentID)
	_, err := adapter.FindWord(ctx, "NON_EXISTENT_TEST_WORD_FOR_VALIDATION")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "not_found") || strings.Contains(errStr, "could not find") {
			fmt.Printf("[Trace] Database %s invalid/missing. Starting repair...\n", currentID)

			// If it was the main database, re-discover
			if currentID == user.NotionConfig.DatabaseID {
				tempAdapter := notion.NewAdapter(token, "")
				if discID, _, _ := tempAdapter.DiscoverDatabaseID(ctx, user.NotionConfig.DatabaseName); discID != "" {
					user.NotionConfig.DatabaseID = discID
					_ = h.userRepo.CreateOrUpdateUser(ctx, user)
					return discID, nil
				}
			}
			return currentID, err // Could not repair, return original so caller can decide
		}
	}

	return currentID, nil
}
