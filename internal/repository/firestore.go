package repository

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/geekabo93/lingofetch/internal/models"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type FirestoreUserRepository struct {
	client *firestore.Client
}

func NewUserRepository(ctx context.Context, projectID, databaseID string) (*FirestoreUserRepository, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required for Firestore")
	}
	if databaseID == "" {
		databaseID = "(default)"
	}
	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to create firestore client for database %s: %w", databaseID, err)
	}
	return &FirestoreUserRepository{client: client}, nil
}

func (r *FirestoreUserRepository) Close() error {
	return r.client.Close()
}

// GetUser retrieves a user by their ID
func (r *FirestoreUserRepository) GetUser(ctx context.Context, userID string) (*models.User, error) {
	doc, err := r.client.Collection("users").Doc(userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}

	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail retrieves a user by their email
func (r *FirestoreUserRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	fmt.Printf("[Firestore] Searching for user by email: %s\n", email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	query := r.client.Collection("users").Where("email", "==", email).Limit(1)
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	var user models.User
	if err := docs[0].DataTo(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateOrUpdateUser saves/updates a user document. It handles setting default values and preserving fields.
func (r *FirestoreUserRepository) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
	if user.ID == "" {
		return fmt.Errorf("user ID is required")
	}

	docRef := r.client.Collection("users").Doc(user.ID)

	// Pre-fill metadata
	user.UpdatedAt = time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	// Double-check if we need to auto-heal from another record with same email
	if user.Email != "" && (user.NotionConfig == nil || user.NotionConfig.DatabaseID == "") {
		if old, err := r.GetUserByEmail(ctx, user.Email); err == nil && old.ID != user.ID {
			// 🛡️ Only heal if it's the SAME database name or if everything is empty
			canHeal := false
			if user.NotionConfig != nil && old.NotionConfig != nil {
				if user.NotionConfig.DatabaseName == old.NotionConfig.DatabaseName {
					canHeal = true
				}
			} else {
				canHeal = true
			}

			if canHeal {
				fmt.Printf("[Firestore] Healing user %s from account %s (Matching Name: %s)\n", user.ID, old.ID, old.NotionConfig.DatabaseName)
				if user.NotionConfig == nil {
					user.NotionConfig = old.NotionConfig
				} else if user.NotionConfig.DatabaseID == "" && old.NotionConfig != nil {
					user.NotionConfig.DatabaseID = old.NotionConfig.DatabaseID
					user.NotionConfig.AccessToken = old.NotionConfig.AccessToken
				}
				if user.AIPrefs == nil {
					user.AIPrefs = old.AIPrefs
				}
			}
		}
	}

	// Prepare data for saving
	data := map[string]interface{}{
		"id":         user.ID,
		"email":      user.Email,
		"updated_at": user.UpdatedAt,
		"created_at": user.CreatedAt,
	}

	if user.NotionConfig != nil {
		data["notion_config"] = map[string]interface{}{
			"access_token":       user.NotionConfig.AccessToken,
			"database_id":        user.NotionConfig.DatabaseID,
			"database_name":      user.NotionConfig.DatabaseName,
			"workspace_name":     user.NotionConfig.WorkspaceName,
			"workspace_icon":     user.NotionConfig.WorkspaceIcon,
			"parent_page_id":     user.NotionConfig.ParentPageID,
			"detected_languages": user.NotionConfig.DetectedLanguages,
			"connected_at":       user.NotionConfig.ConnectedAt,
		}
	} else {
		data["notion_config"] = firestore.Delete
	}

	if user.AIPrefs != nil {
		data["ai_preferences"] = map[string]interface{}{
			"default_provider": user.AIPrefs.DefaultProvider,
			"target_language":  user.AIPrefs.TargetLanguage,
			"custom_api_key":   user.AIPrefs.CustomAPIKey,
		}
	} else {
		data["ai_preferences"] = firestore.Delete
	}

	fmt.Printf("[Firestore] Saving user %s (Email: %s, DB: %s)\n",
		user.ID, user.Email, getDBID(user))

	_, err := docRef.Set(ctx, data, firestore.MergeAll)
	return err
}

// DeleteUser removes a user and their words subcollection
func (r *FirestoreUserRepository) DeleteUser(ctx context.Context, userID string) error {
	// First delete all words in subcollection (Firestore won't do this automatically)
	col := r.client.Collection("users").Doc(userID).Collection("words")
	bulkwriter := r.client.BulkWriter(ctx)
	iter := col.DocumentRefs(ctx)
	for {
		ref, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		bulkwriter.Delete(ref)
	}
	bulkwriter.Flush()

	// Finally delete the user document
	_, err := r.client.Collection("users").Doc(userID).Delete(ctx)
	return err
}

func getDBID(u *models.User) string {
	if u.NotionConfig != nil {
		return u.NotionConfig.DatabaseID
	}
	return "none"
}

// SaveWord saves a word to the user's words subcollection
func (r *FirestoreUserRepository) SaveWord(ctx context.Context, userID string, word *models.WordEntry) error {
	// Create a subcollection Document
	docRef := r.client.Collection("users").Doc(userID).Collection("words").Doc(word.ID)
	_, err := docRef.Set(ctx, word)
	return err
}

// FindWord checks if a word with the same target language already exists for the user
func (r *FirestoreUserRepository) FindWord(ctx context.Context, userID, word, targetLanguage string) (*models.WordEntry, error) {
	query := r.client.Collection("users").Doc(userID).Collection("words").
		Where("word", "==", word).
		Where("target_language", "==", targetLanguage).
		Limit(1)

	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, nil
	}

	var entry models.WordEntry
	if err := docs[0].DataTo(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}
