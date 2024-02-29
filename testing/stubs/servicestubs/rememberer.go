package servicestubs

import (
	"context"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/rememberer"
)

type Rememberer struct {
	GetWordsResponse *rememberer.GetWordsResponse
	GetWordsError    error

	GetWordByIdResponse *rememberer.GetWordByIdResponse
	GetWordByIdError    error

	CreateWordResponse *rememberer.CreateWordResponse
	CreateWordError    error

	DeleteWordByIdError error
}

func (r *Rememberer) DeleteWordById(ctx context.Context, req *rememberer.DeleteWordRequest) error {
	return r.DeleteWordByIdError
}

func (r *Rememberer) CreateWord(ctx context.Context, req *rememberer.CreateWordRequest) (*rememberer.CreateWordResponse, error) {
	return r.CreateWordResponse, r.CreateWordError
}

func (r *Rememberer) GetWordById(ctx context.Context, req *rememberer.GetWordByIdRequest) (*rememberer.GetWordByIdResponse, error) {
	return r.GetWordByIdResponse, r.GetWordByIdError
}

func (r *Rememberer) GetWords(ctx context.Context, req *rememberer.GetWordsRequest) (*rememberer.GetWordsResponse, error) {
	return r.GetWordsResponse, r.GetWordsError
}
