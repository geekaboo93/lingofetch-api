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
	"github.com/geekabo93/lingofetch/internal/sync/obsidian"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// CaptureHandler handles word capture requests and coordinates AI analysis with note provider synchronization.
type CaptureHandler struct {
	cfg          *config.Config
	providers    map[ai.ProviderType]ai.Provider
	defaultType  ai.ProviderType
	userRepo     repository.Repository
	noteProvider sync.NoteProvider // Fallback provider (internal)
}

// NewCaptureHandler initializes a new CaptureHandler with necessary dependencies.
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

// Handle processes the word capture request with concurrent AI and Note provider operations
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

	// 1. RESOLVE USER CONFIG & TOKENS
	var user *models.User
	var decryptedNotionToken string
	var decryptedObsidianToken string
	if req.UserID != "" && h.userRepo != nil {
		u, err := h.userRepo.GetUser(ctx, req.UserID)
		if err == nil {
			user = u
			if user.Notes != nil && user.Notes.Notion != nil && user.Notes.Notion.AccessToken != "" {
				decryptedNotionToken, _ = crypto.Decrypt(user.Notes.Notion.AccessToken, h.cfg.EncryptionKey)
			}
			if user.Notes != nil && user.Notes.Obsidian != nil && user.Notes.Obsidian.AccessToken != "" {
				decryptedObsidianToken, _ = crypto.Decrypt(user.Notes.Obsidian.AccessToken, h.cfg.EncryptionKey)
			}
		}
	}

	// 2. Determine which AI provider to use
	pType := h.defaultType
	providerSource := req.Provider
	if providerSource == "" && user != nil && user.AIPrefs != nil {
		providerSource = user.AIPrefs.DefaultProvider
	}
	if providerSource != "" {
		if pt, err := ai.GetProviderFromString(providerSource); err == nil {
			pType = pt
		}
	}

	var aiProvider ai.Provider
	if req.CustomAPIKey != "" {
		customProvider, err := ai.NewProvider(ctx, ai.ProviderConfig{Type: pType, APIKey: req.CustomAPIKey})
		if err == nil {
			aiProvider = customProvider
		}
	}
	if aiProvider == nil {
		if user != nil && user.AIPrefs != nil && user.AIPrefs.CustomAPIKey != "" {
			k, _ := crypto.Decrypt(user.AIPrefs.CustomAPIKey, h.cfg.EncryptionKey)
			if p, err := ai.NewProvider(ctx, ai.ProviderConfig{Type: pType, APIKey: k}); err == nil {
				aiProvider = p
			}
		}
	}
	if aiProvider == nil {
		var ok bool
		aiProvider, ok = h.providers[pType]
		if !ok {
			aiProvider = h.providers[h.defaultType]
		}
	}

	if aiProvider == nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Success: false, Error: "No AI provider available"})
		return
	}

	// 3. Determine Target Language
	targetLang := req.TargetLanguage
	if targetLang == "" && user != nil && user.AIPrefs != nil {
		targetLang = user.AIPrefs.TargetLanguage
	}
	if targetLang == "" {
		targetLang = "English (US)"
	}

	// 4. CHECK IF WORD ALREADY EXISTS (Local Repository)
	if req.UserID != "" && h.userRepo != nil {
		fmt.Printf("[Trace] Checking repository for existing word: %s\n", req.Word)
		existing, err := h.userRepo.FindWord(ctx, req.UserID, req.Word, targetLang)
		if err == nil && existing != nil {
			msg := resources.GetRandomMessage("mastery")
			c.JSON(http.StatusOK, models.CaptureResponse{
				Success:       true,
				Word:          existing.Word,
				Definition:    existing.Definition,
				Pronunciation: existing.Pronunciation,
				PartOfSpeech:  existing.PartOfSpeech,
				Example:       existing.Example,
				NoteURL:       existing.NoteURL,
				Message:       msg,
				IsDuplicate:   true,
				Timestamp:     existing.CreatedAt,
			})
			return
		}
	}

	// 5. START AI GENERATION
	fmt.Printf("[Trace] Triggering AI analysis using %s...\n", aiProvider.Name())
	definition, err := aiProvider.GenerateDefinition(ctx, req.Word, targetLang, req.Context, req.PageLanguage, req.SourceLanguage)
	if err != nil {
		fmt.Printf("[AI] ERROR: Generation failed for '%s': %v\n", req.Word, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Success: false, Error: err.Error()})
		return
	}
	fmt.Printf("[Trace] AI Analysis Complete: language=%s, isAmbiguous=%v\n", definition.LanguageCode, definition.IsAmbiguous)

	// Ambiguity check
	if definition.IsAmbiguous && len(definition.PossibleLanguages) > 1 {
		c.JSON(http.StatusOK, models.CaptureResponse{
			Success:           true,
			Word:              definition.Word,
			IsAmbiguous:       true,
			PossibleLanguages: definition.PossibleLanguages,
		})
		return
	}

	sourceLang := definition.LanguageCode

	// 6. RESOLVE TARGET NOTE PROVIDER & DUPLICATES
	var targetNoteProvider sync.NoteProvider = h.noteProvider
	var existingURL string

	activeProvider := "notion"
	if user != nil && user.Notes != nil && user.Notes.ActiveProvider != "" {
		activeProvider = user.Notes.ActiveProvider
	}

	// Logic: If active is specified and connected, use it.
	// Otherwise, fallback to Notion if connected, then Obsidian.
	useNotion := (activeProvider == "notion" && decryptedNotionToken != "") ||
		(activeProvider == "obsidian" && decryptedObsidianToken == "" && decryptedNotionToken != "") ||
		(activeProvider == "" && decryptedNotionToken != "")

	useObsidian := (activeProvider == "obsidian" && decryptedObsidianToken != "") ||
		(activeProvider == "notion" && decryptedNotionToken == "" && decryptedObsidianToken != "") ||
		(activeProvider == "" && decryptedNotionToken == "" && decryptedObsidianToken != "")

	if useNotion {
		fmt.Printf("[Sync] Proceeding with Notion. Token Present: %v\n", decryptedNotionToken != "")
		notionDbID, suggest := h.resolveNotionDatabase(ctx, user, sourceLang, targetLang)
		fmt.Printf("[Sync] Resolved Notion DB: %s (Suggest: %v)\n", notionDbID, suggest)

		// Validate existence and repair if needed
		notionDbID, _ = h.ensureDatabaseValid(ctx, user, decryptedNotionToken, notionDbID)
		fmt.Printf("[Sync] Post-Validation Notion DB: %s\n", notionDbID)

		if notionDbID != "" {
			fmt.Printf("[Sync] Using Notion database: %s\n", notionDbID)
			adapter := notion.NewAdapter(decryptedNotionToken, notionDbID)
			targetNoteProvider = adapter
			existingURL, _ = adapter.FindWord(ctx, definition.Word)

			if existingURL != "" {
				fmt.Printf("[Sync] Duplicate found in Notion: %s\n", existingURL)
				h.finishDuplicate(c, definition, existingURL, sourceLang)
				return
			}
		} else if !suggest {
			// Main language but no database found/created
			fmt.Printf("[Sync] CRITICAL: Could not resolve or create Notion database for main language.\n")
			h.finishHoldSync(c, definition, sourceLang, true)
			return
		}

		if suggest && existingURL == "" {
			fmt.Printf("[Sync] Secondary language detected (%s). Auto-creating database...\n", sourceLang)
			// Automatically create language-specific database
			newDbID, err := h.handleAutoCreateNotion(ctx, user, decryptedNotionToken, sourceLang)
			if err == nil {
				notionDbID = newDbID
				// Re-init adapter with new DB ID
				adapter := notion.NewAdapter(decryptedNotionToken, notionDbID)
				targetNoteProvider = adapter
				fmt.Printf("[Sync] SUCCESS: Auto-created Notion database for %s: %s\n", sourceLang, notionDbID)
			} else {
				fmt.Printf("[Sync] ERROR: Failed to auto-create Notion database: %v\n", err)
				h.finishHoldSync(c, definition, sourceLang, true)
				return
			}
		}
	} else if useObsidian {
		obsidianPath, suggest := h.resolveObsidianPath(user, sourceLang, targetLang)
		adapter := obsidian.NewAdapter(user.Notes.Obsidian.BaseURL, decryptedObsidianToken, obsidianPath)
		targetNoteProvider = adapter
		existingURL, _ = adapter.FindWord(ctx, definition.Word)

		if existingURL != "" {
			h.finishDuplicate(c, definition, existingURL, sourceLang)
			return
		}
		if suggest && existingURL == "" {
			// Automatically create language-specific path
			newPath, err := h.handleAutoCreateObsidian(ctx, user, sourceLang)
			if err == nil {
				obsidianPath = newPath
				// Re-init adapter with new path
				adapter := obsidian.NewAdapter(user.Notes.Obsidian.BaseURL, decryptedObsidianToken, obsidianPath)
				targetNoteProvider = adapter
				fmt.Printf("[Sync] Auto-created Obsidian path for %s: %s\n", sourceLang, obsidianPath)
			} else {
				fmt.Printf("[Sync] Failed to auto-create Obsidian path: %v\n", err)
				h.finishHoldSync(c, definition, sourceLang, true)
				return
			}
		}
	}

	// 7. RESPOND (Normal case)
	c.JSON(http.StatusOK, models.CaptureResponse{
		Success:        true,
		Word:           definition.Word,
		Definition:     definition.Definition,
		Pronunciation:  definition.Pronunciation,
		PartOfSpeech:   definition.PartOfSpeech,
		Example:        definition.Example,
		SourceLanguage: sourceLang,
		Timestamp:      time.Now(),
	})

	// 8. BACKGROUND SYNC
	// We run this in a background goroutine to avoid blocking the client response.
	// We use context.Background() because the request context will be cancelled when the handler returns.
	go func() {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer bgCancel()

		fmt.Printf("[Sync] Starting background tasks for word: %s\n", definition.Word)

		wordEntry := &models.WordEntry{
			ID:             uuid.New().String(),
			Word:           definition.Word,
			Definition:     definition.Definition,
			Pronunciation:  definition.Pronunciation,
			PartOfSpeech:   definition.PartOfSpeech,
			Example:        definition.Example,
			TargetLanguage: targetLang,
			SourceLanguage: sourceLang,
			CreatedAt:      time.Now(),
		}

		g, gCtx := errgroup.WithContext(bgCtx)

		// Task 1: Save to Note Provider
		if targetNoteProvider != nil {
			g.Go(func() error {
				fmt.Printf("[Sync] Saving word to note provider (%s)...\n", targetNoteProvider.Name())
				noteURL, err := targetNoteProvider.SaveWord(gCtx, wordEntry)
				if err != nil {
					return fmt.Errorf("note provider %s save failed: %w", targetNoteProvider.Name(), err)
				}
				wordEntry.NoteURL = noteURL
				fmt.Printf("[Sync] SUCCESS: Saved to Note Provider\n")
				return nil
			})
		}

		// Task 2: Save to Repository (Firestore)
		if req.UserID != "" && h.userRepo != nil {
			g.Go(func() error {
				fmt.Printf("[Sync] Saving word to repository...\n")
				if err := h.userRepo.SaveWord(gCtx, req.UserID, wordEntry); err != nil {
					return fmt.Errorf("repository save failed: %w", err)
				}
				fmt.Printf("[Sync] SUCCESS: Saved to Repository\n")
				return nil
			})
		}

		// Wait for all background tasks to complete or for context to timeout
		if err := g.Wait(); err != nil {
			fmt.Printf("[Sync] Background sync error: %v\n", err)
		} else {
			fmt.Printf("[Sync] Background sync finished successfully\n")
		}
	}()
}

func (h *CaptureHandler) finishDuplicate(c *gin.Context, definition *ai.WordAnalysis, url, sourceLang string) {
	msg := resources.GetRandomMessage("mastery")
	c.JSON(http.StatusOK, models.CaptureResponse{
		Success:        true,
		Word:           definition.Word,
		Definition:     definition.Definition,
		Pronunciation:  definition.Pronunciation,
		PartOfSpeech:   definition.PartOfSpeech,
		Example:        definition.Example,
		NoteURL:        url,
		Message:        msg,
		IsDuplicate:    true,
		SourceLanguage: sourceLang,
		Timestamp:      time.Now(),
	})
}

func (h *CaptureHandler) finishHoldSync(c *gin.Context, definition *ai.WordAnalysis, sourceLang string, suggest bool) {
	c.JSON(http.StatusOK, models.CaptureResponse{
		Success:        true,
		Word:           definition.Word,
		Definition:     definition.Definition,
		Pronunciation:  definition.Pronunciation,
		PartOfSpeech:   definition.PartOfSpeech,
		Example:        definition.Example,
		SourceLanguage: sourceLang,
		SuggestRouting: suggest,
		HoldSync:       true,
		Timestamp:      time.Now(),
	})
}

func (h *CaptureHandler) resolveNotionDatabase(ctx context.Context, user *models.User, sourceLang, targetLang string) (string, bool) {
	if user == nil || user.Notes.Notion == nil {
		return "", false
	}
	shortLang := languages.GetLanguageCode(sourceLang)
	targetCode := languages.GetLanguageCode(targetLang)

	if user.Notes.Notion.DetectedLanguages != nil {
		if route, ok := user.Notes.Notion.DetectedLanguages[sourceLang]; ok && route.DatabaseID != "" {
			return route.DatabaseID, false
		}
		if route, ok := user.Notes.Notion.DetectedLanguages[shortLang]; ok && route.DatabaseID != "" {
			return route.DatabaseID, false
		}
	}

	// Stay on default if it matches target language or if it's explicitly English (universal fallback)
	isMainLanguage := shortLang == targetCode || shortLang == "en"
	return user.Notes.Notion.DefaultDatabaseID, sourceLang != "" && !isMainLanguage
}

func (h *CaptureHandler) resolveObsidianPath(user *models.User, sourceLang, targetLang string) (string, bool) {
	if user == nil || user.Notes.Obsidian == nil {
		return fmt.Sprintf("LingoFetch/%s/%s.base", models.DefaultDatabaseName, models.DefaultDatabaseName), false
	}
	shortLang := languages.GetLanguageCode(sourceLang)
	targetCode := languages.GetLanguageCode(targetLang)

	if user.Notes.Obsidian.DetectedLanguages != nil {
		if route, ok := user.Notes.Obsidian.DetectedLanguages[sourceLang]; ok && route.DatabaseID != "" {
			return route.DatabaseID, false
		}
		if route, ok := user.Notes.Obsidian.DetectedLanguages[shortLang]; ok && route.DatabaseID != "" {
			return route.DatabaseID, false
		}
	}

	path := fmt.Sprintf("LingoFetch/%s/%s.base", models.DefaultDatabaseName, models.DefaultDatabaseName)
	if user.Notes.Obsidian.DefaultDatabaseID != "" {
		path = user.Notes.Obsidian.DefaultDatabaseID
	}

	isMainLanguage := shortLang == targetCode || shortLang == "en"
	return path, sourceLang != "" && !isMainLanguage
}

func (h *CaptureHandler) handleAutoCreateNotion(ctx context.Context, user *models.User, token, langCode string) (string, error) {
	if user == nil || user.Notes == nil || user.Notes.Notion == nil {
		return "", fmt.Errorf("user config incomplete")
	}

	pageID := user.Notes.Notion.ParentPageID
	adapter := notion.NewAdapter(token, "")

	if pageID == "" {
		// Try to discover it
		_, discoveredPageID, err := adapter.DiscoverDatabaseID(ctx, models.DefaultDatabaseName)
		if err == nil && discoveredPageID != "" {
			pageID = discoveredPageID
			user.Notes.Notion.ParentPageID = pageID
		}
	}

	if pageID == "" {
		return "", fmt.Errorf("parent page not found")
	}

	langName := languages.GetLanguageName(langCode)
	dbName := fmt.Sprintf("LingoFetch [%s]", strings.ToUpper(langCode))

	newDbID, err := adapter.CreateDatabase(ctx, pageID, dbName)
	if err != nil {
		return "", err
	}

	if user.Notes.Notion.DetectedLanguages == nil {
		user.Notes.Notion.DetectedLanguages = make(map[string]models.LanguageRoute)
	}
	user.Notes.Notion.DetectedLanguages[langCode] = models.LanguageRoute{
		DatabaseID:   newDbID,
		DatabaseName: dbName,
		LanguageName: langName,
	}

	_ = h.userRepo.CreateOrUpdateUser(ctx, user)
	return newDbID, nil
}

func (h *CaptureHandler) handleAutoCreateObsidian(ctx context.Context, user *models.User, langCode string) (string, error) {
	if user == nil || user.Notes == nil || user.Notes.Obsidian == nil {
		return "", fmt.Errorf("user config incomplete")
	}

	langName := languages.GetLanguageName(langCode)
	dbName := fmt.Sprintf("LingoFetch [%s]", strings.ToUpper(langCode))
	basePath := fmt.Sprintf("LingoFetch/%s/%s.base", dbName, dbName)

	if user.Notes.Obsidian.DetectedLanguages == nil {
		user.Notes.Obsidian.DetectedLanguages = make(map[string]models.LanguageRoute)
	}
	user.Notes.Obsidian.DetectedLanguages[langCode] = models.LanguageRoute{
		DatabaseID:   basePath,
		DatabaseName: dbName,
		LanguageName: langName,
	}

	_ = h.userRepo.CreateOrUpdateUser(ctx, user)
	return basePath, nil
}

func (h *CaptureHandler) ensureDatabaseValid(ctx context.Context, user *models.User, token string, currentID string) (string, error) {
	if user == nil || user.Notes.Notion == nil {
		return currentID, nil
	}

	// 1. If we have an ID, check if it's reachable and correct
	if currentID != "" {
		tempAdapter := notion.NewAdapter(token, currentID)
		_, err := tempAdapter.FindWord(ctx, "existence-check-probe") // Minimal search check
		if err == nil {
			return currentID, nil
		}
		fmt.Printf("[Sync] Database %s is unreachable or invalid: %v. Attempting recovery...\n", currentID, err)
	}

	// 2. If currentID was invalid or empty, try the DefaultDatabaseID if different
	if currentID != user.Notes.Notion.DefaultDatabaseID && user.Notes.Notion.DefaultDatabaseID != "" {
		tempAdapter := notion.NewAdapter(token, user.Notes.Notion.DefaultDatabaseID)
		_, err := tempAdapter.FindWord(ctx, "existence-check-probe")
		if err == nil {
			return user.Notes.Notion.DefaultDatabaseID, nil
		}
		fmt.Printf("[Sync] Default database ID %s is also unreachable.\n", user.Notes.Notion.DefaultDatabaseID)
	}

	// 3. Last Resort: discovery or creation
	tempAdapter := notion.NewAdapter(token, "")
	targetName := user.Notes.Notion.DefaultDatabaseName
	if targetName == "" {
		targetName = models.DefaultDatabaseName
	}

	fmt.Printf("[Sync] Starting deep discovery for database: '%s'...\n", targetName)
	discID, pageID, err := tempAdapter.DiscoverDatabaseID(ctx, targetName)
	if err == nil {
		if discID != "" {
			fmt.Printf("[Sync] Discovery found database: %s\n", discID)
			user.Notes.Notion.DefaultDatabaseID = discID
			if pageID != "" {
				user.Notes.Notion.ParentPageID = pageID
			}
			_ = h.userRepo.CreateOrUpdateUser(ctx, user)
			return discID, nil
		} else if pageID != "" {
			fmt.Printf("[Sync] Discovery found parent page: %s. Creating new database...\n", pageID)
			user.Notes.Notion.ParentPageID = pageID
			if newDbID, err := tempAdapter.CreateDatabase(ctx, pageID, targetName); err == nil {
				fmt.Printf("[Sync] Successfully created database: %s\n", newDbID)
				user.Notes.Notion.DefaultDatabaseID = newDbID
				_ = h.userRepo.CreateOrUpdateUser(ctx, user)
				return newDbID, nil
			} else {
				fmt.Printf("[Sync] ERROR: Database creation failed: %v\n", err)
			}
		}
	} else {
		fmt.Printf("[Sync] ERROR: Discovery failed: %v\n", err)
	}

	return "", fmt.Errorf("could not resolve or create Notion database")
}
