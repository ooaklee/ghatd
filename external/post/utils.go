package post

import (
	"crypto/sha256"
	"fmt"
)

// GenerateSha256Hash creates a SHA-256 hash of the input string
func GenerateSha256Hash(input string) string {
	hash := sha256.New()
	hash.Write([]byte(input))
	return fmt.Sprintf("%x", hash.Sum(nil))
}
