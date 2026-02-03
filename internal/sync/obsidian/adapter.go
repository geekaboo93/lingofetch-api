package obsidian

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/geekabo93/lingofetch/internal/pkg/languages"
)

type Adapter struct {
	baseURL     string
	accessToken string
	targetPath  string // The path to the .base file
	client      *http.Client
}

func NewAdapter(baseURL, accessToken, targetPath string) *Adapter {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:27123"
	}

	// 🐳 Docker Awareness
	if _, err := os.Stat("/.dockerenv"); err == nil {
		if strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") {
			baseURL = strings.Replace(baseURL, "localhost", "host.docker.internal", 1)
			baseURL = strings.Replace(baseURL, "127.0.0.1", "host.docker.internal", 1)
		}
	}

	return &Adapter{
		baseURL:     strings.TrimSuffix(baseURL, "/"),
		accessToken: accessToken,
		targetPath:  targetPath,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *Adapter) Name() string {
	return "Obsidian"
}

// SaveWord handles syncing a word by creating a new Markdown file for it
func (a *Adapter) SaveWord(ctx context.Context, word *models.WordEntry) (string, error) {
	// 1. Ensure the .base file and folder structure exists
	exists, _ := a.fileExists(ctx, a.targetPath)
	if !exists {
		_, _ = a.CreateDatabase(ctx, a.targetPath)
	}

	// 2. Determine word file path
	// If base is "LingoFetch/LingoFetch Dictionary.base", word file is "LingoFetch/Words/WordName.md"
	baseDir := ""
	if idx := strings.LastIndex(a.targetPath, "/"); idx != -1 {
		baseDir = a.targetPath[:idx+1]
	}
	wordFolder := baseDir + "Words"

	// Create the word file path
	fileName := word.Word
	// Sanitize filename
	fileName = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, fileName)
	wordPath := fmt.Sprintf("%s/%s.md", wordFolder, fileName)

	// 3. Create YAML-only content for the word file
	// This format is best for 'Bases' and 'Dataview'
	content := fmt.Sprintf(`---
Word: "%s"
Definition: "%s"
Pronunciation: "%s"
POS: "%s"
Example: "%s"
Source: "%s"
Captured: %s
---
`,
		strings.ReplaceAll(word.Word, `"`, `\"`),
		strings.ReplaceAll(word.Definition, `"`, `\"`),
		strings.ReplaceAll(word.Pronunciation, `"`, `\"`),
		strings.ReplaceAll(word.PartOfSpeech, `"`, `\"`),
		strings.ReplaceAll(word.Example, `"`, `\"`),
		strings.ReplaceAll(languages.GetLanguageName(word.SourceLanguage), `"`, `\"`),
		word.CreatedAt.Format("2006-01-02 15:04"),
	)

	escapedWordPath := a.escapePath(wordPath)
	apiURL := fmt.Sprintf("%s/vault/%s", a.baseURL, escapedWordPath)

	req, _ := http.NewRequestWithContext(ctx, "PUT", apiURL, bytes.NewBufferString(content))
	req.Header.Add("Authorization", "Bearer "+a.accessToken)
	req.Header.Add("Content-Type", "text/markdown")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to sync word to obsidian: %s", string(body))
	}

	return fmt.Sprintf("obsidian://open?file=%s", url.QueryEscape(wordPath)), nil
}

// Helper to check if file exists
func (a *Adapter) fileExists(ctx context.Context, path string) (bool, error) {
	escapedPath := a.escapePath(path)
	apiURL := fmt.Sprintf("%s/vault/%s", a.baseURL, escapedPath)

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Add("Authorization", "Bearer "+a.accessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// Helper to escape path segments
func (a *Adapter) escapePath(path string) string {
	pathParts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, p := range pathParts {
		pathParts[i] = url.PathEscape(p)
	}
	return strings.Join(pathParts, "/")
}

func (a *Adapter) GetWords(ctx context.Context, limit int) ([]*models.WordEntry, error) {
	return []*models.WordEntry{}, nil
}

func (a *Adapter) FindWord(ctx context.Context, word string) (string, error) {
	baseDir := ""
	if idx := strings.LastIndex(a.targetPath, "/"); idx != -1 {
		baseDir = a.targetPath[:idx+1]
	}
	wordPath := fmt.Sprintf("%sWords/%s.md", baseDir, word)

	exists, _ := a.fileExists(ctx, wordPath)
	if exists {
		return fmt.Sprintf("obsidian://open?file=%s", url.QueryEscape(wordPath)), nil
	}
	return "", nil
}

// CreateDatabase initializes the .base file with a YAML view configuration
func (a *Adapter) CreateDatabase(ctx context.Context, path string) (string, error) {
	// Ensure path ends in .base
	finalPath := path
	if !strings.HasSuffix(strings.ToLower(path), ".base") {
		finalPath = strings.TrimSuffix(path, ".md") + ".base"
	}

	baseDir := ""
	if idx := strings.LastIndex(finalPath, "/"); idx != -1 {
		baseDir = finalPath[:idx+1]
	}
	wordFolder := baseDir + "Words"

	// Define a simple 'Bases' YAML structure
	// This points to files in the Words/ folder
	// Define a strict 'Bases' YAML structure
	// 'order' controls which columns are visible by default in the view
	viewConfig := fmt.Sprintf(`properties:
  file.name:
    displayName: "Word"
  Definition:
    displayName: "Definition"
  Pronunciation:
    displayName: "Pronunciation"
  POS:
    displayName: "POS"
  Example:
    displayName: "Example"
  Captured:
    displayName: "Captured"
views:
  - name: Vocabulary List
    type: table
    filters: file.inFolder("%s")
    order:
      - file.name
      - Definition
      - Pronunciation
      - POS
      - Example
      - Captured
`, wordFolder)

	escapedPath := a.escapePath(finalPath)
	apiURL := fmt.Sprintf("%s/vault/%s", a.baseURL, escapedPath)

	req, _ := http.NewRequestWithContext(ctx, "PUT", apiURL, bytes.NewBufferString(viewConfig))
	req.Header.Add("Authorization", "Bearer "+a.accessToken)
	req.Header.Add("Content-Type", "application/yaml")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create base: %s", string(body))
	}

	return finalPath, nil
}

// VerifyConnection checks if the base URL and access token are valid
func (a *Adapter) VerifyConnection(ctx context.Context) error {
	apiURL := fmt.Sprintf("%s/", a.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+a.accessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to obsidian: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("obsidian returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
