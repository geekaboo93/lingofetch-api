package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/geekabo93/lingofetch/internal/config"
	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/geekabo93/lingofetch/internal/pkg/crypto"
	"github.com/geekabo93/lingofetch/internal/pkg/resources"
	"github.com/geekabo93/lingofetch/internal/repository"
	notionadapter "github.com/geekabo93/lingofetch/internal/sync/notion"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	cfg      *config.Config
	userRepo repository.Repository
}

func NewAuthHandler(cfg *config.Config, userRepo repository.Repository) *AuthHandler {
	return &AuthHandler{
		cfg:      cfg,
		userRepo: userRepo,
	}
}

// NotionLogin redirects the user to Notion's OAuth page
func (h *AuthHandler) NotionLogin(c *gin.Context) {
	userID := c.Query("user_id")
	dbName := c.DefaultQuery("db_name", "LingoFetch Dictionary")

	fmt.Printf("[Auth] Login request: user=%s, db_name=%s\n", userID, dbName)

	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	if h.cfg.NotionClientID == "" || h.cfg.NotionRedirectURI == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Notion OAuth credentials are not configured in the backend"})
		return
	}

	state := userID + "|" + dbName

	authURL := fmt.Sprintf(
		"https://api.notion.com/v1/oauth/authorize?client_id=%s&response_type=code&owner=user&redirect_uri=%s&state=%s",
		h.cfg.NotionClientID,
		url.QueryEscape(h.cfg.NotionRedirectURI),
		url.QueryEscape(state),
	)

	fmt.Printf("[Auth] Redirecting to Notion: %s\n", authURL)
	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

// NotionCallback handles the redirect from Notion
func (h *AuthHandler) NotionCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code and state are required"})
		return
	}

	// Parse state: userID|dbName
	parts := strings.Split(state, "|")
	userID := parts[0]
	targetDBName := "LingoFetch Dictionary"
	if len(parts) > 1 {
		targetDBName = parts[1]
	}
	fmt.Printf("[Auth] Derived target database name: '%s'\n", targetDBName)

	// Exchange code for access token
	tokenResponse, err := h.exchangeCode(code)
	if err != nil {
		fmt.Printf("[Auth] Error exchanging code: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to exchange code: %v", err)})
		return
	}

	// Encrypt the access token
	encryptedToken, err := crypto.Encrypt(tokenResponse.AccessToken, h.cfg.EncryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("encryption failed: %v", err)})
		return
	}

	// Save or Update user in Firestore
	user := &models.User{
		ID:    userID,
		Email: tokenResponse.Owner.User.Person.Email,
		NotionConfig: &models.NotionConfig{
			AccessToken:   encryptedToken,
			DatabaseID:    tokenResponse.DuplicatedTemplateID,
			DatabaseName:  targetDBName,
			WorkspaceName: tokenResponse.WorkspaceName,
			WorkspaceIcon: tokenResponse.WorkspaceIcon,
			ConnectedAt:   time.Now(),
		},
	}

	fmt.Printf("[Auth] User Model Prepared: ID=%s, Email=%s, TargetDBName=%s, ExistingDBID=%s\n",
		user.ID, user.Email, user.NotionConfig.DatabaseName, user.NotionConfig.DatabaseID)

	// 🛠️ IDENTITY REUSE LOGIC: If a user with this email already exists, migrate/reuse their data
	if existingUser, err := h.userRepo.GetUserByEmail(context.Background(), user.Email); err == nil {
		fmt.Printf("[Auth] Found existing matching user %s for email %s.\n", existingUser.ID, user.Email)

		// 1. Inherit AI preferences from existing user if not already set by the new session
		if existingUser.AIPrefs != nil {
			if user.AIPrefs == nil {
				user.AIPrefs = existingUser.AIPrefs
			} else {
				// Merge: keep new provider if set, but take target language if missing
				if user.AIPrefs.DefaultProvider == "" {
					user.AIPrefs.DefaultProvider = existingUser.AIPrefs.DefaultProvider
				}
				if user.AIPrefs.TargetLanguage == "" {
					user.AIPrefs.TargetLanguage = existingUser.AIPrefs.TargetLanguage
				}
			}
		}

		// 2. Inherit Notion configuration
		if existingUser.NotionConfig != nil {
			// If the user's old dictionary had a custom name, and they didn't rename it in this new popup yet, restore it
			if user.NotionConfig.DatabaseName == "LingoFetch Dictionary" && existingUser.NotionConfig.DatabaseName != "" {
				user.NotionConfig.DatabaseName = existingUser.NotionConfig.DatabaseName
			}

			if user.NotionConfig.DatabaseID == "" {
				user.NotionConfig.DatabaseID = existingUser.NotionConfig.DatabaseID
			}
			if user.NotionConfig.ParentPageID == "" {
				user.NotionConfig.ParentPageID = existingUser.NotionConfig.ParentPageID
			}
			user.NotionConfig.DetectedLanguages = existingUser.NotionConfig.DetectedLanguages
		}

		// 3. If IDs different, cleanup old session record
		if existingUser.ID != userID {
			fmt.Printf("[Auth] Migrating from old ID %s to new ID %s.\n", existingUser.ID, userID)
			if err := h.userRepo.DeleteUser(context.Background(), existingUser.ID); err != nil {
				fmt.Printf("[Auth] Warning: failed to delete old user record %s: %v\n", existingUser.ID, err)
			}
		}
	} else {
		// If NO existing user by email, check if we already have some preferences saved for this NEW ID
		if currentNewUser, err := h.userRepo.GetUser(context.Background(), userID); err == nil {
			if currentNewUser.AIPrefs != nil && user.AIPrefs == nil {
				user.AIPrefs = currentNewUser.AIPrefs
			}
		}
	}

	// 🛠️ DISCOVERY LOGIC: Try to find or create the target dictionary if we still don't have a DB ID
	if user.NotionConfig.DatabaseID == "" {
		fmt.Printf("[Auth] Starting discovery for '%s'...\n", targetDBName)
		ctx := context.Background()
		adapter := notionadapter.NewAdapter(tokenResponse.AccessToken, "")
		dbID, pageID, err := adapter.DiscoverDatabaseID(ctx, targetDBName)
		if err == nil {
			// Always save pageID if found, as it serves as the parent for future language DBs
			if pageID != "" {
				user.NotionConfig.ParentPageID = pageID
				fmt.Printf("[Auth] Captured ParentPageID: %s\n", pageID)
			}

			if dbID != "" {
				user.NotionConfig.DatabaseID = dbID
				fmt.Printf("[Auth] SUCCESS: Found matching database: %s\n", dbID)
			} else if pageID != "" {
				fmt.Printf("[Auth] INFO: No database found, but found parent page %s. Creating new dictionary '%s'...\n", pageID, targetDBName)
				if newDbID, err := adapter.CreateDatabase(ctx, pageID, targetDBName); err == nil {
					user.NotionConfig.DatabaseID = newDbID
					fmt.Printf("[Auth] SUCCESS: Created new database: %s\n", newDbID)
				}
			}
		} else {
			fmt.Printf("[Auth] ERROR: Discovery failed: %v\n", err)
		}
	}

	fmt.Printf("[Auth] Final User Save: ID=%s, DB_Name=%s, DB_ID=%s\n",
		user.ID, user.NotionConfig.DatabaseName, user.NotionConfig.DatabaseID)

	if err := h.userRepo.CreateOrUpdateUser(context.Background(), user); err != nil {
		fmt.Printf("[Auth] FINAL ERROR: Failed to save user: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save user config: %v", err)})
		return
	}
	fmt.Printf("[Auth] Successfully saved config for user: %s\n", userID)

	// HTML response from embedded resource file
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, resources.GetAuthSuccessHTML())
}

type notionTokenResponse struct {
	AccessToken          string `json:"access_token"`
	WorkspaceName        string `json:"workspace_name"`
	WorkspaceIcon        string `json:"workspace_icon"`
	WorkspaceID          string `json:"workspace_id"`
	DuplicatedTemplateID string `json:"duplicated_template_id"`
	Owner                struct {
		User struct {
			Person struct {
				Email string `json:"email"`
			} `json:"person"`
		} `json:"user"`
	} `json:"owner"`
}

func (h *AuthHandler) exchangeCode(code string) (*notionTokenResponse, error) {
	body := map[string]string{
		"grant_type":   "authorization_code",
		"code":         code,
		"redirect_uri": h.cfg.NotionRedirectURI,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", "https://api.notion.com/v1/oauth/token", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", h.cfg.NotionClientID, h.cfg.NotionClientSecret)))
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Notion-Version", "2022-06-28")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("notion error: %v (status %d)", errResp, resp.StatusCode)
	}

	var result notionTokenResponse
	rawBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("[Auth] Raw Notion Response: %s\n", string(rawBody))
	if err := json.Unmarshal(rawBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
