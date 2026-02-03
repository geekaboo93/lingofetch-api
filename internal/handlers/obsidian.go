package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/geekabo93/lingofetch/internal/config"
	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/geekabo93/lingofetch/internal/pkg/crypto"
	"github.com/geekabo93/lingofetch/internal/pkg/resources"
	"github.com/geekabo93/lingofetch/internal/repository"
	"github.com/geekabo93/lingofetch/internal/sync/obsidian"
	"github.com/gin-gonic/gin"
)

type ObsidianHandler struct {
	cfg      *config.Config
	userRepo repository.Repository
}

func NewObsidianHandler(cfg *config.Config, userRepo repository.Repository) *ObsidianHandler {
	return &ObsidianHandler{
		cfg:      cfg,
		userRepo: userRepo,
	}
}

// GetStatus checks Obsidian connection
func (h *ObsidianHandler) GetStatus(c *gin.Context) {
	userID := c.Query("user_id")
	success := c.Query("success")

	if userID == "" {
		c.JSON(http.StatusOK, gin.H{"connected": false})
		return
	}

	user, err := h.userRepo.GetUser(c.Request.Context(), userID)
	if err != nil || user.Notes == nil || user.Notes.Obsidian == nil {
		c.JSON(http.StatusOK, gin.H{"connected": false})
		return
	}

	if success == "true" {
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, resources.GetAuthSuccessHTML("Obsidian"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"connected":             user.Notes != nil && user.Notes.Obsidian != nil && user.Notes.Obsidian.AccessToken != "",
		"base_url":              user.Notes.Obsidian.BaseURL,
		"default_database_id":   user.Notes.Obsidian.DefaultDatabaseID,
		"default_database_name": user.Notes.Obsidian.DefaultDatabaseName,
		"detected_languages":    user.Notes.Obsidian.DetectedLanguages,
	})
}

// UpdateSettings updates Obsidian configuration
func (h *ObsidianHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		UserID              string `json:"user_id" binding:"required"`
		BaseURL             string `json:"base_url"`
		AccessToken         string `json:"access_token"`
		DefaultDatabaseID   string `json:"default_database_id"`
		DefaultDatabaseName string `json:"default_database_name"`
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

	if user.Notes == nil {
		user.Notes = &models.NotesConfig{}
	}
	user.Notes.ActiveProvider = "obsidian"
	if user.Notes.Obsidian == nil {
		user.Notes.Obsidian = &models.ObsidianConfig{
			ConnectedAt:       time.Now(),
			DetectedLanguages: make(map[string]models.LanguageRoute),
		}
	}

	if req.BaseURL != "" {
		user.Notes.Obsidian.BaseURL = req.BaseURL
	}
	if req.DefaultDatabaseID != "" {
		user.Notes.Obsidian.DefaultDatabaseID = req.DefaultDatabaseID
	}
	if req.DefaultDatabaseName != "" {
		user.Notes.Obsidian.DefaultDatabaseName = req.DefaultDatabaseName
	}
	if user.Notes.Obsidian.DefaultDatabaseName == "" {
		user.Notes.Obsidian.DefaultDatabaseName = models.DefaultDatabaseName
	}
	if req.AccessToken != "" {
		encrypted, err := crypto.Encrypt(req.AccessToken, h.cfg.EncryptionKey)
		if err != nil {
			log.Printf("[Obsidian] Encryption failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
			return
		}
		user.Notes.Obsidian.AccessToken = encrypted

		// Verify connection
		adapter := obsidian.NewAdapter(user.Notes.Obsidian.BaseURL, req.AccessToken, "")
		if err := adapter.VerifyConnection(ctx); err != nil {
			log.Printf("[Obsidian] Connection verification failed for user %s: %v", req.UserID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to connect to Obsidian: %v", err)})
			return
		}
	}

	if err := h.userRepo.CreateOrUpdateUser(ctx, user); err != nil {
		log.Printf("[Obsidian] Failed to save settings for user %s: %v", req.UserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SyncWord syncs a word to Obsidian
func (h *ObsidianHandler) SyncWord(c *gin.Context) {
	var req struct {
		UserID    string           `json:"user_id" binding:"required"`
		WordEntry models.WordEntry `json:"word_entry" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil || user.Notes.Obsidian == nil || user.Notes.Obsidian.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Obsidian not connected"})
		return
	}

	token, _ := crypto.Decrypt(user.Notes.Obsidian.AccessToken, h.cfg.EncryptionKey)

	targetPath := ""
	if user.Notes.Obsidian.DetectedLanguages != nil {
		if route, ok := user.Notes.Obsidian.DetectedLanguages[req.WordEntry.SourceLanguage]; ok {
			targetPath = route.DatabaseID
		}
	}
	if targetPath == "" {
		targetPath = fmt.Sprintf("LingoFetch/%s/%s.base", models.DefaultDatabaseName, models.DefaultDatabaseName)
	}

	adapter := obsidian.NewAdapter(user.Notes.Obsidian.BaseURL, token, targetPath)
	url, err := adapter.SaveWord(ctx, &req.WordEntry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"url":     url,
	})
}

// RemoveDatabase unlinks an Obsidian file
func (h *ObsidianHandler) RemoveDatabase(c *gin.Context) {
	var req struct {
		UserID       string `json:"user_id" binding:"required"`
		LanguageCode string `json:"language_code" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.GetUser(ctx, req.UserID)
	if err != nil || user.Notes.Obsidian == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user or obsidian config not found"})
		return
	}

	if user.Notes.Obsidian.DetectedLanguages != nil {
		delete(user.Notes.Obsidian.DetectedLanguages, req.LanguageCode)
		h.userRepo.CreateOrUpdateUser(ctx, user)
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
