package repositorystubs

import (
	"context"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/rememberer"
)

type Repository struct {
	GetWordsResponse []rememberer.Word
	GetWordsError    error

	GetWordByIdResponse *rememberer.Word
	GetWordByIdError    error

	GetWordByNameResponse *rememberer.Word
	GetWordByNameError    error

	CreateWordResponse *rememberer.Word
	CreateWordError    error

	DeleteWordByIdError error

	CreateWordsResponse []rememberer.Word
	CreateWordsError    error
}

func (r *Repository) GetWordByName(ctx context.Context, name string) (*rememberer.Word, error) {
	return r.GetWordByNameResponse, r.GetWordByNameError
}

func (r Repository) DeleteWordById(ctx context.Context, id string) error {
	return r.DeleteWordByIdError
}

func (r Repository) CreateWord(ctx context.Context, name string) (*rememberer.Word, error) {
	return r.CreateWordResponse, r.CreateWordError
}

func (r Repository) GetWordById(ctx context.Context, id string) (*rememberer.Word, error) {
	return r.GetWordByIdResponse, r.GetWordByIdError
}

func (r Repository) GetWords(ctx context.Context) ([]rememberer.Word, error) {
	return r.GetWordsResponse, r.GetWordsError
}
