package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/geekabo93/lingofetch/internal/models"
)

// Repository defines the interface for user data storage
type Repository interface {
	GetUser(ctx context.Context, userID string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateOrUpdateUser(ctx context.Context, user *models.User) error
	SaveWord(ctx context.Context, userID string, word *models.WordEntry) error
	FindWord(ctx context.Context, userID, word, targetLanguage string) (*models.WordEntry, error)
	DeleteUser(ctx context.Context, userID string) error
	Close() error
}

// MemoryUserRepository is an in-memory implementation of the Repository interface for local testing
type MemoryUserRepository struct {
	users map[string]*models.User
	words map[string][]*models.WordEntry // userID -> list of words
	mu    sync.RWMutex
}

func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{
		users: make(map[string]*models.User),
		words: make(map[string][]*models.WordEntry),
	}
}

func (r *MemoryUserRepository) GetUser(ctx context.Context, userID string) (*models.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[userID]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	return user, nil
}

func (r *MemoryUserRepository) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, user := range r.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (r *MemoryUserRepository) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	user.UpdatedAt = time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	r.users[user.ID] = user
	return nil
}

func (r *MemoryUserRepository) SaveWord(ctx context.Context, userID string, word *models.WordEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.words[userID] = append(r.words[userID], word)
	return nil
}

func (r *MemoryUserRepository) FindWord(ctx context.Context, userID, word, targetLanguage string) (*models.WordEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	searchWord := strings.ToLower(strings.TrimSpace(word))
	for _, w := range r.words[userID] {
		// Both should be normalized
		if strings.ToLower(w.Word) == searchWord && w.TargetLanguage == targetLanguage {
			return w, nil
		}
	}
	return nil, nil
}

func (r *MemoryUserRepository) DeleteUser(ctx context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.users, userID)
	delete(r.words, userID)
	return nil
}

func (r *MemoryUserRepository) Close() error {
	return nil
}
