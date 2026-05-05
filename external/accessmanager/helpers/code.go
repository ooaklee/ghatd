package accessmanagerhelpers

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply"
	"go.uber.org/zap"
)

const (
	codeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength   = 8
	maxRetries   = 5

	ErrKeyCodeGenerationFailure = "CodeGenerationFailure"
)

// CodeManagementErrorMap holds error keys and their corresponding messages for code management operations
var CodeManagementErrorMap = reply.ErrorManifest{
	ErrKeyCodeGenerationFailure: {
		Title:      "Internal Error",
		Detail:     "Failed to generate a unique code. Please try again.",
		StatusCode: 500,
		Code:       "AMH00-001",
	},
}

// CodeStore expects methods required for unique code management in ephemeral storage
type CodeStore interface {
	CodeExists(ctx context.Context, code string) (bool, error)
	StoreCode(ctx context.Context, code string, ttl time.Duration) error
}

// GenerateUniqueCode produces a globally unique 8-character alphanumeric code
// (A-Z, 0-9, case-insensitive). It checks ephemeral storage for collisions and
// retries with a new code if a collision is detected.
func GenerateUniqueCode(ctx context.Context, store CodeStore, ttl time.Duration) (string, error) {

	log := logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	for attempt := 0; attempt < maxRetries; attempt++ {
		code, err := generateRandomCode()
		if err != nil {
			log.Error("amh/failed-to-generate-random-code", zap.Int("attempt", attempt), zap.Error(err))
			return "", err
		}

		exists, err := store.CodeExists(ctx, code)
		if err != nil {
			log.Error("amh/failed-to-check-code-existence", zap.String("code", code), zap.Error(err))
			return "", errors.New(ErrKeyCodeGenerationFailure)
		}

		if !exists {
			err = store.StoreCode(ctx, code, ttl)
			if err != nil {
				log.Error("amh/failed-to-store-code", zap.String("code", code), zap.Error(err))
				return "", errors.New(ErrKeyCodeGenerationFailure)
			}

			return code, nil
		}

		log.Warn("amh/code-collision-detected-retrying", zap.Int("attempt", attempt))
	}

	log.Error("amh/exceeded-max-code-generation-retries", zap.Int("max-retries", maxRetries))
	return "", errors.New(ErrKeyCodeGenerationFailure)
}

func generateRandomCode() (string, error) {
	code := make([]byte, codeLength)
	alphabetLen := big.NewInt(int64(len(codeAlphabet)))

	for i := range code {
		idx, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", err
		}
		code[i] = codeAlphabet[idx.Int64()]
	}

	return string(code), nil
}
