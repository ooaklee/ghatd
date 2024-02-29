package repository

import (
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/rememberer"
)

// InMememoryRepository holds the methods for managing words
type InMememoryRepository struct {
	// Store holds "persistent" words
	Store []rememberer.Word
}

// NewInMememoryRepository returns a new in-memory repository
func NewInMememoryRepository() *InMememoryRepository {

	return &InMememoryRepository{
		Store: []rememberer.Word{},
	}
}
