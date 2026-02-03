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

// FirestoreUserRepository implements the Repository interface using Google Cloud Firestore.
// It handles user profile management and word persistent storage.
type FirestoreUserRepository struct {
	client *firestore.Client
}

// NewUserRepository creates and initializes a new FirestoreUserRepository.
// It requires a Google Cloud project ID and optionally a database ID (defaults to "(default)").
func NewUserRepository(ctx context.Context, projectID, databaseID string) (*FirestoreUserRepository, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID is required for Firestore")
	}
	if databaseID == "" {
		databaseID = "(default)"
	}
	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to create firestore client for project %s, database %s: %w", projectID, databaseID, err)
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

// CreateOrUpdateUser saves or updates a user document. It implements identity healing
// to ensure account settings are preserved across different login sessions.
func (r *FirestoreUserRepository) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
	if user.ID == "" {
		return fmt.Errorf("user ID is required")
	}

	docRef := r.client.Collection("users").Doc(user.ID)

	// Update metadata
	user.UpdatedAt = time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	// 🛠️ IDENTITY HEALING: Merges data from a potential legacy account linked to the same email.
	// This prevents data loss during provider transitions (e.g., connecting Notion after Obsidian).
	if user.Email != "" && (user.Notes == nil || user.Notes.Notion == nil || user.Notes.Notion.DefaultDatabaseID == "") {
		if old, err := r.GetUserByEmail(ctx, user.Email); err == nil && old.ID != user.ID {
			fmt.Printf("[Firestore] Healing user %s data from legacy account: %s\n", user.ID, old.ID)
			r.healUserData(user, old)
		}
	}

	// Persist to Firestore using struct tags
	if _, err := docRef.Set(ctx, user); err != nil {
		fmt.Printf("[Firestore] Error: failed to persist user %s to firestore: %v\n", user.ID, err)
		return fmt.Errorf("failed to persist user %s to firestore: %w", user.ID, err)
	}

	return nil
}

// healUserData merges critical configuration fields from a legacy user record into the current one.
func (r *FirestoreUserRepository) healUserData(user, old *models.User) {
	if old.Notes == nil {
		return
	}

	if user.Notes == nil {
		user.Notes = old.Notes
		return
	}

	// Merge Notion settings
	if old.Notes.Notion != nil {
		if user.Notes.Notion == nil {
			user.Notes.Notion = old.Notes.Notion
		} else {
			// Backfill missing critical Notion fields
			if user.Notes.Notion.DefaultDatabaseID == "" {
				user.Notes.Notion.DefaultDatabaseID = old.Notes.Notion.DefaultDatabaseID
			}
			if user.Notes.Notion.DefaultDatabaseName == "" {
				user.Notes.Notion.DefaultDatabaseName = old.Notes.Notion.DefaultDatabaseName
			}
			if user.Notes.Notion.ParentPageID == "" {
				user.Notes.Notion.ParentPageID = old.Notes.Notion.ParentPageID
			}
			if user.Notes.Notion.AccessToken == "" {
				user.Notes.Notion.AccessToken = old.Notes.Notion.AccessToken
			}

			// Merge detected language routes
			if old.Notes.Notion.DetectedLanguages != nil {
				if user.Notes.Notion.DetectedLanguages == nil {
					user.Notes.Notion.DetectedLanguages = make(map[string]models.LanguageRoute)
				}
				for lang, route := range old.Notes.Notion.DetectedLanguages {
					if _, exists := user.Notes.Notion.DetectedLanguages[lang]; !exists {
						user.Notes.Notion.DetectedLanguages[lang] = route
					}
				}
			}
		}
	}

	// Merge Obsidian settings
	if old.Notes.Obsidian != nil {
		if user.Notes.Obsidian == nil {
			user.Notes.Obsidian = old.Notes.Obsidian
		} else {
			if user.Notes.Obsidian.AccessToken == "" {
				user.Notes.Obsidian.AccessToken = old.Notes.Obsidian.AccessToken
			}
			if user.Notes.Obsidian.DefaultDatabaseID == "" {
				user.Notes.Obsidian.DefaultDatabaseID = old.Notes.Obsidian.DefaultDatabaseID
			}
		}
	}

	// Merge AI preferences
	if old.AIPrefs != nil && user.AIPrefs == nil {
		user.AIPrefs = old.AIPrefs
	}
}

// DeleteUser removes a user and their words subcollection
func (r *FirestoreUserRepository) DeleteUser(ctx context.Context, userID string) error {
	col := r.client.Collection("users").Doc(userID).Collection("words")
	bulkwriter := r.client.BulkWriter(ctx)
	iter := col.DocumentRefs(ctx)
	for {
		docRef, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		_, err = bulkwriter.Delete(docRef)
		if err != nil {
			return err
		}
	}
	bulkwriter.End()

	_, err := r.client.Collection("users").Doc(userID).Delete(ctx)
	return err
}

func (r *FirestoreUserRepository) SaveWord(ctx context.Context, userID string, word *models.WordEntry) error {
	_, err := r.client.Collection("users").Doc(userID).Collection("words").Doc(word.ID).Set(ctx, word)
	return err
}

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
