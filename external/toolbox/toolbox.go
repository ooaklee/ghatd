package toolbox

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

const ()

// TimeNowUTC returns time as string in RFC3339 format w/o timezone
func TimeNowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.999999999")
}

// GenerateNanoId is returning a nano id
func GenerateNanoId() string {
	// nolint no need to check error as not returned
	id, _ := gonanoid.New()

	return id
}

// GenerateUuidV4 returns Uuidv4 string
func GenerateUuidV4() string {
	// nolint no need to check error as not returned
	uid, _ := uuid.NewRandom()

	return uid.String()
}

// OutputBasicLogString attempts to output log in a JSON format otherwise will default to plain text
func OutputBasicLogString(level, message string) string {
	type ServerBasicError struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		TimeStamp string `json:"ts"`
	}

	basicError := ServerBasicError{
		Level:     level,
		Message:   message,
		TimeStamp: TimeNowUTC(),
	}

	marshalledJson, err := json.Marshal(basicError)
	if err != nil {
		return message
	}

	return string(marshalledJson)
}

// StringRemoveMultiSpace subsitutes all multispace with single space
func StringRemoveMultiSpace(s string) string {
	multipleSpaceRegex := regexp.MustCompile(`\s\s+`)

	return multipleSpaceRegex.ReplaceAllString(s, " ")
}

// StringStandardisedToLower returns a string with no explicit spacing strategy
// that is all lowercase and standardised.
func StringStandardisedToLower(s string) string {
	s = strings.ToLower(s)

	return StringRemoveMultiSpace(strings.TrimSpace(strings.ReplaceAll(s, "’", "'")))
}

// DecodeRequestBody attempts to decode request to object. returns error on failure
func DecodeRequestBody(request *http.Request, parsedRequestObject interface{}) error {
	return json.NewDecoder(request.Body).Decode(parsedRequestObject)
}

// StripNonAlphanumericCharactersRegex handles remove all non-alpanumeric charactes
// from passed string
func StripNonAlphanumericCharactersRegex(in []byte, with []byte) string {
	reg, _ := regexp.Compile("[^a-zA-Z0-9 ]+")
	return reg.ReplaceAllString(string(in), string(with))
}
