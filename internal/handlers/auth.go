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

// AuthHandler manages authentication flows for different note providers (Notion, Obsidian).
type AuthHandler struct {
	cfg      *config.Config
	userRepo repository.Repository
}

// NewAuthHandler creates a new AuthHandler instance.
func NewAuthHandler(cfg *config.Config, userRepo repository.Repository) *AuthHandler {
	return &AuthHandler{
		cfg:      cfg,
		userRepo: userRepo,
	}
}

// NotionLogin redirects the user to Notion's OAuth page
func (h *AuthHandler) NotionLogin(c *gin.Context) {
	userID := c.Query("user_id")
	dbName := c.DefaultQuery("db_name", models.DefaultDatabaseName)

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

// ObsidianLogin serves the connection UI for Obsidian
func (h *AuthHandler) ObsidianLogin(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, resources.GetObsidianConnectHTML(userID))
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
	targetDBName := models.DefaultDatabaseName
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

	// 🔄 IDENTITY HEALING: Load existing user or merge from another account with same email.
	user, err := h.userRepo.GetUser(c.Request.Context(), userID)
	if err != nil {
		fmt.Printf("[Auth] No existing user for ID=%s, creating or searching by email...\n", userID)
		user = &models.User{
			ID:        userID,
			Email:     tokenResponse.Owner.User.Person.Email,
			CreatedAt: time.Now(),
		}
	}

	// Always update common fields
	user.Email = tokenResponse.Owner.User.Person.Email
	user.UpdatedAt = time.Now()

	// Initialize config structures
	if user.Notes == nil {
		user.Notes = &models.NotesConfig{}
	}
	user.Notes.ActiveProvider = "notion"

	if user.Notes.Notion == nil {
		user.Notes.Notion = &models.NotionConfig{}
	}

	// Use CreateOrUpdateUser which now handles deep healing logic.
	// This keeps the handler clean and moves the complex merge logic to the repository layer.

	// 3. Update Notion-specific fields from current OAuth session
	user.Notes.Notion.AccessToken = encryptedToken
	user.Notes.Notion.WorkspaceName = tokenResponse.WorkspaceName
	user.Notes.Notion.WorkspaceIcon = tokenResponse.WorkspaceIcon
	user.Notes.Notion.ConnectedAt = time.Now()

	// 4. Handle Database ID/Name persistence
	if user.Notes.Notion.DefaultDatabaseName == "" || user.Notes.Notion.DefaultDatabaseName == models.DefaultDatabaseName {
		user.Notes.Notion.DefaultDatabaseName = targetDBName
	}
	if user.Notes.Notion.DefaultDatabaseID == "" {
		user.Notes.Notion.DefaultDatabaseID = tokenResponse.DuplicatedTemplateID
	}

	// 🛠️ DISCOVERY LOGIC: Try to find or create the target dictionary if we still don't have a DB ID or Parent Page
	if user.Notes.Notion.DefaultDatabaseID == "" || user.Notes.Notion.ParentPageID == "" {
		fmt.Printf("[Auth] Starting discovery for '%s'...\n", targetDBName)
		ctx := context.Background()
		adapter := notionadapter.NewAdapter(tokenResponse.AccessToken, "")
		dbID, pageID, err := adapter.DiscoverDatabaseID(ctx, targetDBName)
		if err == nil {
			// Always save pageID if found, as it serves as the parent for future language DBs
			if pageID != "" {
				user.Notes.Notion.ParentPageID = pageID
				fmt.Printf("[Auth] Captured ParentPageID: %s\n", pageID)
			}

			if dbID != "" {
				user.Notes.Notion.DefaultDatabaseID = dbID
				fmt.Printf("[Auth] SUCCESS: Found matching database: %s\n", dbID)
			} else if pageID != "" {
				fmt.Printf("[Auth] INFO: No database found, but found parent page %s. Creating new dictionary '%s'...\n", pageID, targetDBName)
				if newDbID, err := adapter.CreateDatabase(ctx, pageID, targetDBName); err == nil {
					user.Notes.Notion.DefaultDatabaseID = newDbID
					fmt.Printf("[Auth] SUCCESS: Created new database: %s\n", newDbID)
				}
			}
		} else {
			fmt.Printf("[Auth] ERROR: Discovery failed: %v\n", err)
		}
	}

	fmt.Printf("[Auth] Final User Save: ID=%s, DB_Name=%s, DB_ID=%s\n",
		user.ID, user.Notes.Notion.DefaultDatabaseName, user.Notes.Notion.DefaultDatabaseID)

	if err := h.userRepo.CreateOrUpdateUser(context.Background(), user); err != nil {
		fmt.Printf("[Auth] FINAL ERROR: Failed to save user: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save user config: %v", err)})
		return
	}
	fmt.Printf("[Auth] Successfully saved config for user: %s\n", userID)

	// HTML response from embedded resource file
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, resources.GetAuthSuccessHTML("Notion"))
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
