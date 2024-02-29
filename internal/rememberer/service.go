package rememberer

import (
	"context"
	"errors"
	"fmt"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/logger"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/toolbox"
)

// remembererRespository expected methods of a valid rememberer repository
type remembererRespository interface {
	GetWords(ctx context.Context) ([]Word, error)
	CreateWord(ctx context.Context, word string) (*Word, error)
	GetWordById(ctx context.Context, id string) (*Word, error)
	GetWordByName(ctx context.Context, name string) (*Word, error)
	DeleteWordById(ctx context.Context, id string) error
}

// Service holds and manages rememberer business logic
type Service struct {
	remembererRespository remembererRespository
}

// NewService created rememberer service
func NewService(remembererRespository remembererRespository) *Service {
	return &Service{
		remembererRespository: remembererRespository,
	}
}

// DeleteWordById attempts to delete the word with matching Id in repository
func (s *Service) DeleteWordById(ctx context.Context, r *DeleteWordRequest) error {

	logger := logger.AcquireFrom(ctx)

	logger.Info(fmt.Sprintf("checking-for-word-to-delete-with-id: %s", r.Id))
	_, err := s.remembererRespository.GetWordById(ctx, r.Id)
	if err != nil {

		return err
	}

	logger.Debug(fmt.Sprintf("found-word-with-id: %s", r.Id))

	logger.Info(fmt.Sprintf("requesting-repository-deletes-word-with-id: %s", r.Id))
	return s.remembererRespository.DeleteWordById(ctx, r.Id)
}

// CreateWord attempt to create the word in the repository
func (s *Service) CreateWord(ctx context.Context, r *CreateWordRequest) (*CreateWordResponse, error) {

	logger := logger.AcquireFrom(ctx)

	normalisedName := toolbox.StringStandardisedToLower(r.Name)

	logger.Info(fmt.Sprintf("creating-new-word-entry: %s", normalisedName))

	_, err := s.remembererRespository.GetWordByName(ctx, normalisedName)
	if err == nil {
		logger.Warn(fmt.Sprintf("word-entry-with-name-exists: %s", normalisedName))
		return nil, errors.New(ErrKeyWordAlreadyExists)
	}

	word, err := s.remembererRespository.CreateWord(ctx, normalisedName)
	if err != nil {
		return nil, err
	}

	logger.Info(fmt.Sprintf("sucessfully-created-new-entry-for-word: %s (%s)", word.Name, word.Id))

	return &CreateWordResponse{
		Word: word,
	}, nil
}

// GetWords returns the words from the repository
func (s *Service) GetWords(ctx context.Context, r *GetWordsRequest) (*GetWordsResponse, error) {
	logger := logger.AcquireFrom(ctx)

	logger.Info("requesting-all-words-in-repository")

	words, err := s.remembererRespository.GetWords(ctx)
	if err != nil {
		logger.Warn("failed-to-get-all-word-entries")
		return nil, err
	}

	return &GetWordsResponse{
		Words: words,
	}, nil
}

// GetWordById returns word with matching Id from repository
func (s *Service) GetWordById(ctx context.Context, r *GetWordByIdRequest) (*GetWordByIdResponse, error) {
	logger := logger.AcquireFrom(ctx)

	logger.Info(fmt.Sprintf("checking-for-word-to-get-with-id: %s", r.Id))
	word, err := s.remembererRespository.GetWordById(ctx, r.Id)
	if err != nil {

		return nil, err
	}

	logger.Info(fmt.Sprintf("sucessfully-found-entry-with-id: %s ", word.Id))
	return &GetWordByIdResponse{
		Word: word,
	}, nil
}
