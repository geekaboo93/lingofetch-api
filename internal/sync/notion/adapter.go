package notion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/geekabo93/lingofetch/internal/models"
	"github.com/jomei/notionapi"
)

// Adapter implements the NoteProvider interface for Notion
type Adapter struct {
	client     *notionapi.Client
	databaseID notionapi.DatabaseID
}

// DatabaseInfo represents a simplified Notion database structure
type DatabaseInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ListDatabases returns all databases accessible by the integration
func (a *Adapter) ListDatabases(ctx context.Context) ([]DatabaseInfo, error) {
	var list []DatabaseInfo
	var cursor notionapi.Cursor

	for {
		res, err := a.client.Search.Do(ctx, &notionapi.SearchRequest{
			Filter: notionapi.SearchFilter{
				Property: "object",
				Value:    "database",
			},
			StartCursor: cursor,
			PageSize:    100,
		})
		if err != nil {
			return nil, err
		}

		for _, result := range res.Results {
			if db, ok := result.(*notionapi.Database); ok {
				title := "Untitled"
				if len(db.Title) > 0 {
					title = db.Title[0].PlainText
				}
				list = append(list, DatabaseInfo{
					ID:    string(db.ID),
					Title: title,
				})
			}
		}

		if !res.HasMore {
			break
		}
		cursor = res.NextCursor
	}

	return list, nil
}

// NewAdapter creates a new Notion adapter
func NewAdapter(apiKey, databaseID string) *Adapter {
	return &Adapter{
		client:     notionapi.NewClient(notionapi.Token(apiKey)),
		databaseID: notionapi.DatabaseID(databaseID),
	}
}

// Name returns the provider name
func (a *Adapter) Name() string {
	return "Notion"
}

// SaveWord saves a word entry to Notion database
func (a *Adapter) SaveWord(ctx context.Context, word *models.WordEntry) (string, error) {
	db, err := a.client.Database.Get(ctx, a.databaseID)
	if err != nil {
		return "", fmt.Errorf("failed to get database schema for ID '%s': %w", a.databaseID, err)
	}

	dbTitle := ""
	if len(db.Title) > 0 {
		dbTitle = db.Title[0].PlainText
	}
	fmt.Printf("[Debug] Saving word to database: '%s' (ID: %s)\n", dbTitle, a.databaseID)

	// Helper to check if property exists
	fmt.Printf("[Debug] Available Properties in Database: ")
	for propName := range db.Properties {
		fmt.Printf("'%s' ", propName)
	}
	fmt.Println()

	// Find which property is the title (usually "Word" or "Name")
	var titlePropName string
	for name, config := range db.Properties {
		if config.GetType() == "title" {
			titlePropName = name
			break
		}
	}

	if titlePropName == "" {
		return "", fmt.Errorf("could not find title property in database")
	}

	// Create properties map
	props := notionapi.Properties{
		titlePropName: notionapi.TitleProperty{
			Title: []notionapi.RichText{{Text: &notionapi.Text{Content: word.Word}}},
		},
	}

	// helper function to handle case-insensitive property addition
	addProp := func(name string, value notionapi.Property) bool {
		nameLower := strings.ToLower(name)
		for actualName := range db.Properties {
			if strings.ToLower(actualName) == nameLower {
				props[actualName] = value
				return true
			}
		}
		return false
	}

	// Only add properties that exist in the actual Notion database schema
	addProp("Definition", notionapi.RichTextProperty{
		RichText: []notionapi.RichText{{Text: &notionapi.Text{Content: word.Definition}}},
	})
	addProp("Pronunciation", notionapi.RichTextProperty{
		RichText: []notionapi.RichText{{Text: &notionapi.Text{Content: word.Pronunciation}}},
	})

	if !addProp("Part of Speech", notionapi.SelectProperty{
		Select: notionapi.Option{Name: word.PartOfSpeech},
	}) {
		addProp("Type", notionapi.SelectProperty{
			Select: notionapi.Option{Name: word.PartOfSpeech},
		})
	}

	addProp("Example", notionapi.RichTextProperty{
		RichText: []notionapi.RichText{{Text: &notionapi.Text{Content: word.Example}}},
	})
	addProp("🕒 Created", notionapi.DateProperty{
		Date: &notionapi.DateObject{Start: (*notionapi.Date)(&word.CreatedAt)},
	})

	pageRequest := &notionapi.PageCreateRequest{
		Parent:     notionapi.Parent{Type: notionapi.ParentTypeDatabaseID, DatabaseID: a.databaseID},
		Properties: props,
	}

	// Create the page
	page, err := a.client.Page.Create(ctx, pageRequest)
	if err != nil {
		return "", fmt.Errorf("failed to create Notion page: %w", err)
	}

	fmt.Printf("[Debug] Created Notion page: %s\n", page.URL)
	return string(page.URL), nil
}

// FindWord checks if a word already exists in the Notion database
func (a *Adapter) FindWord(ctx context.Context, word string) (string, error) {
	// 1. Get database schema to find the title property name
	db, err := a.client.Database.Get(ctx, a.databaseID)
	if err != nil {
		return "", fmt.Errorf("failed to get database schema: %w", err)
	}

	var titlePropName string
	for name, config := range db.Properties {
		if config.GetType() == "title" {
			titlePropName = name
			break
		}
	}

	if titlePropName == "" {
		return "", fmt.Errorf("could not find title property")
	}

	// 2. Query for the word
	queryRequest := &notionapi.DatabaseQueryRequest{
		Filter: &notionapi.PropertyFilter{
			Property: titlePropName,
			RichText: &notionapi.TextFilterCondition{
				Equals: word,
			},
		},
		PageSize: 1,
	}

	res, err := a.client.Database.Query(ctx, a.databaseID, queryRequest)
	if err != nil {
		return "", fmt.Errorf("failed to query database: %w", err)
	}

	if len(res.Results) > 0 {
		return string(res.Results[0].URL), nil
	}

	return "", nil
}

// GetWords retrieves recent word entries from Notion database
func (a *Adapter) GetWords(ctx context.Context, limit int) ([]*models.WordEntry, error) {
	// Query the database
	queryRequest := &notionapi.DatabaseQueryRequest{
		Sorts: []notionapi.SortObject{
			{
				Property:  "🕒 Created",
				Direction: notionapi.SortOrderDESC,
			},
		},
		PageSize: limit,
	}

	response, err := a.client.Database.Query(ctx, a.databaseID, queryRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to query Notion database: %w", err)
	}

	// Convert Notion pages to WordEntry models
	words := make([]*models.WordEntry, 0, len(response.Results))
	for _, page := range response.Results {
		word := &models.WordEntry{
			ID:        string(page.ID),
			NotionURL: string(page.URL),
		}

		// Extract properties
		if title, ok := page.Properties["Word"].(*notionapi.TitleProperty); ok && len(title.Title) > 0 {
			word.Word = title.Title[0].PlainText
		}

		if def, ok := page.Properties["Definition"].(*notionapi.RichTextProperty); ok && len(def.RichText) > 0 {
			word.Definition = def.RichText[0].PlainText
		}

		if pron, ok := page.Properties["Pronunciation"].(*notionapi.RichTextProperty); ok && len(pron.RichText) > 0 {
			word.Pronunciation = pron.RichText[0].PlainText
		}

		if pos, ok := page.Properties["Part of Speech"].(*notionapi.SelectProperty); ok && pos.Select.Name != "" {
			word.PartOfSpeech = pos.Select.Name
		}

		if example, ok := page.Properties["Example"].(*notionapi.RichTextProperty); ok && len(example.RichText) > 0 {
			word.Example = example.RichText[0].PlainText
		}

		if created, ok := page.Properties["🕒 Created"].(*notionapi.DateProperty); ok && created.Date != nil {
			word.CreatedAt = time.Time(*created.Date.Start)
		}

		words = append(words, word)
	}

	return words, nil
}

// DiscoverDatabaseID attempts to find a suitable database in the user's workspace
// DiscoverDatabaseID attempts to find a suitable database.
// It returns (databaseID, pageID, error).
func (a *Adapter) DiscoverDatabaseID(ctx context.Context, targetName string) (string, string, error) {
	if targetName == "" {
		targetName = "LingoFetch Dictionary"
	}
	fmt.Printf("[Debug] [DISCOVERY] Starting discovery for database name: '%s'\n", targetName)

	var res *notionapi.SearchResponse
	var err error
	var firstPageID string

	// Strategy 1: Search for the specific database name
	fmt.Printf("[Debug] [DISCOVERY] Strategy 1: Searching for database with name: '%s'...\n", targetName)
	res, err = a.client.Search.Do(ctx, &notionapi.SearchRequest{
		Query: targetName,
		Filter: notionapi.SearchFilter{
			Property: "object",
			Value:    "database",
		},
		PageSize: 50,
	})
	if err == nil && res != nil {
		fmt.Printf("[Debug] [DISCOVERY] Strategy 1 found %d databases\n", len(res.Results))
		if id, _ := findInResults(res, targetName); id != "" {
			return id, "", nil
		}
	} else if err != nil {
		fmt.Printf("[Debug] [DISCOVERY] Strategy 1 Error: %v\n", err)
	}

	// Strategy 2: Search for Pages specifically (to use as parent for auto-creation)
	fmt.Printf("[Debug] [DISCOVERY] Strategy 2: Searching for any Pages...\n")
	res, err = a.client.Search.Do(ctx, &notionapi.SearchRequest{
		Filter: notionapi.SearchFilter{
			Property: "object",
			Value:    "page",
		},
		PageSize: 50,
	})
	if err == nil && res != nil {
		fmt.Printf("[Debug] [DISCOVERY] Strategy 2 found %d pages\n", len(res.Results))
		// Log what pages we found
		for i, result := range res.Results {
			if pg, ok := result.(*notionapi.Page); ok {
				title := "Untitled Page"
				if tProp, ok := pg.Properties["title"].(*notionapi.TitleProperty); ok && len(tProp.Title) > 0 {
					title = tProp.Title[0].PlainText
				}

				isChildOfDatabase := pg.Parent.Type == notionapi.ParentTypeDatabaseID
				fmt.Printf("[Debug] [DISCOVERY] Found Page[%d]: '%s' (%s) [Parent: %s]\n", i, title, pg.ID, pg.Parent.Type)

				// 🛡️ CRITICAL: Never pick a page that is actually a row in a database
				if !isChildOfDatabase {
					if firstPageID == "" {
						firstPageID = string(pg.ID)
					}
					// If we find a page that actually has a name, prefer that over "Untitled"
					if title != "Untitled Page" {
						firstPageID = string(pg.ID)
						fmt.Printf("[Debug] [DISCOVERY] Found better parent page candidate: '%s'\n", title)
					}
				}
			}
		}
	} else if err != nil {
		fmt.Printf("[Debug] [DISCOVERY] Strategy 2 Error: %v\n", err)
	}

	// Strategy 3: Search by query with object filter (Safe fallback)
	fmt.Printf("[Debug] [DISCOVERY] Strategy 3: Querying for '%s' (fallback)...\n", targetName)
	res, err = a.client.Search.Do(ctx, &notionapi.SearchRequest{
		Query: targetName,
		Filter: notionapi.SearchFilter{
			Property: "object",
			Value:    "database",
		},
		PageSize: 20,
	})
	if err == nil && res != nil && len(res.Results) > 0 {
		if id, _ := findInResults(res, targetName); id != "" {
			return id, "", nil
		}
	}

	if firstPageID != "" {
		fmt.Printf("[Debug] [DISCOVERY] No database found, but will attempt auto-creation on Page: %s\n", firstPageID)
		return "", firstPageID, nil
	}

	return "", "", fmt.Errorf("no shared databases or pages found. Please ensure you have selected a page in the Notion 'Select Pages' screen.")
}

func findInResults(res *notionapi.SearchResponse, targetName string) (string, string) {
	var firstPageID string
	var firstExactMatch string
	var firstExactMatchParent string

	for _, result := range res.Results {
		if db, ok := result.(*notionapi.Database); ok {
			title := ""
			if len(db.Title) > 0 {
				title = db.Title[0].PlainText
			}
			fmt.Printf("[Debug] [DISCOVERY] Found database: '%s' (ID: %s) [Created: %s]\n", title, db.ID, db.CreatedTime)

			if strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(targetName)) {
				if firstExactMatch == "" {
					firstExactMatch = string(db.ID)
					if db.Parent.Type == notionapi.ParentTypePageID {
						firstExactMatchParent = string(db.Parent.PageID)
					}
				}
			}
		} else if page, ok := result.(*notionapi.Page); ok {
			if firstPageID == "" {
				firstPageID = string(page.ID)
			}
		}
	}

	if firstExactMatch != "" {
		return firstExactMatch, firstExactMatchParent
	}
	return "", firstPageID
}

// CreateDatabase creates a new vocabulary database as a child of a page
func (a *Adapter) CreateDatabase(ctx context.Context, pageID string, targetName string) (string, error) {
	if targetName == "" {
		targetName = "LingoFetch Dictionary"
	}
	fmt.Printf("[Debug] Attempting to create '%s' on page: %s\n", targetName, pageID)

	req := &notionapi.DatabaseCreateRequest{
		Parent: notionapi.Parent{
			Type:   notionapi.ParentTypePageID,
			PageID: notionapi.PageID(pageID),
		},
		Title: []notionapi.RichText{
			{Text: &notionapi.Text{Content: targetName}},
		},
		Properties: notionapi.PropertyConfigs{
			"Word": &notionapi.TitlePropertyConfig{
				Type: "title",
			},
			"Definition": &notionapi.RichTextPropertyConfig{
				Type: "rich_text",
			},
			"Part of Speech": &notionapi.SelectPropertyConfig{
				Type: "select",
				Select: notionapi.Select{
					Options: []notionapi.Option{
						{Name: "noun", Color: "blue"},
						{Name: "verb", Color: "green"},
						{Name: "adjective", Color: "yellow"},
						{Name: "adverb", Color: "purple"},
					},
				},
			},
			"Example": &notionapi.RichTextPropertyConfig{
				Type: "rich_text",
			},
			"Pronunciation": &notionapi.RichTextPropertyConfig{
				Type: "rich_text",
			},
			"🕒 Created": &notionapi.DatePropertyConfig{
				Type: "date",
			},
		},
	}

	db, err := a.client.Database.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create database: %w", err)
	}

	fmt.Printf("[Debug] SUCCESS! Created Database with ID: %s\n", db.ID)
	return string(db.ID), nil
}

// RenameDatabase updates the title of a Notion database
func (a *Adapter) RenameDatabase(ctx context.Context, newName string) error {
	if a.databaseID == "" {
		return fmt.Errorf("database ID is required for rename")
	}

	req := &notionapi.DatabaseUpdateRequest{
		Title: []notionapi.RichText{
			{Text: &notionapi.Text{Content: newName}},
		},
	}

	_, err := a.client.Database.Update(ctx, a.databaseID, req)
	if err != nil {
		return fmt.Errorf("failed to rename Notion database: %w", err)
	}

	fmt.Printf("[Debug] Successfully renamed database %s to '%s'\n", a.databaseID, newName)
	return nil
}
