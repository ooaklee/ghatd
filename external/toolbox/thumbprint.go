package toolbox

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"strings"
)

// BytesMatch compares whether bytes match
func BytesMatch(sliceOne, sliceTwo []byte) bool {
	res := bytes.Compare(sliceOne, sliceTwo)

	return res == 0
}

// GenerateThumbprint returns thumbprinted text
func GenerateThumbprint(text string) []byte {
	return []byte(getMD5Hash(getFormatted(text)))
}

// getFormatted returns string in a standardised format for thumbprinting
func getFormatted(text string) string {
	return StringConvertToSnakeCase(StringStandardisedToLower(strings.TrimSpace(text)))
}

// getMD5Hash encodes string as md5 hash
func getMD5Hash(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}
