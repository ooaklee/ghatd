package accessmanagerhelpers

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
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
	ErrCodeGenerationFailure: {
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

	logger := logger.AcquirePackageFrom(ctx, "external/accessmanager/helpers")

	for attempt := 0; attempt < maxRetries; attempt++ {
		code, err := generateRandomCode()
		if err != nil {
			logger.Error("failed-to-generate-random-code", zap.Int("attempt", attempt), zap.Error(err))
			return "", err
		}

		exists, err := store.CodeExists(ctx, code)
		if err != nil {
			logger.Error("failed-to-check-code-existence", zap.Int("attempt", attempt), zap.Bool("code-present", code != ""), zap.Int("code-length", len(code)), zap.Error(err))
			return "", ErrCodeGenerationFailure
		}

		if !exists {
			err = store.StoreCode(ctx, code, ttl)
			if err != nil {
				logger.Error("failed-to-store-code", zap.Int("attempt", attempt), zap.Bool("code-present", code != ""), zap.Int("code-length", len(code)), zap.Error(err))
				return "", ErrCodeGenerationFailure
			}

			return code, nil
		}

		logger.Warn("code-collision-detected-retrying", zap.Int("attempt", attempt))
	}

	logger.Error("exceeded-max-code-generation-retries", zap.Int("max-retries", maxRetries))
	return "", ErrCodeGenerationFailure
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
