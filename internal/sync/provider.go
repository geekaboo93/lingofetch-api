package sync

import (
	"context"

	"github.com/geekabo93/lingofetch/internal/models"
)

// NoteProvider defines the interface for note-taking app integrations
// This adapter pattern allows easy addition of new providers (Obsidian, Evernote, etc.)
type NoteProvider interface {
	// SaveWord saves a word entry to the note-taking app
	SaveWord(ctx context.Context, word *models.WordEntry) (string, error)

	// GetWords retrieves recent word entries from the note-taking app
	GetWords(ctx context.Context, limit int) ([]*models.WordEntry, error)

	// FindWord checks if a word already exists in the provider
	FindWord(ctx context.Context, word string) (string, error)

	// Name returns the provider name
	Name() string
}
