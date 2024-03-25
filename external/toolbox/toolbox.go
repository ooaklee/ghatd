package toolbox

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
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

// Refactor handles replacing any occurrence of the first (old) string with the second (new)
// string in any file(s) that match the provided pattern. This is achieved through a recursive
// process that ensures all relevant files are modified.
// Sourced from https://gist.github.com/jrkt/53f0bd40108d585eaac4c3675b7c1726 and altered
func Refactor(silent bool, old, new, searchPath string, patterns ...string) error {
	if searchPath == "" {
		searchPath = "."
	}
	return filepath.Walk(searchPath, refactorFunc(silent, old, new, patterns))
}

// refactorFunc handles applying recur
func refactorFunc(silent bool, old, new string, filePatterns []string) filepath.WalkFunc {
	return filepath.WalkFunc(func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !!fi.IsDir() {
			return nil
		}

		var matched bool
		for _, pattern := range filePatterns {
			var err error
			matched, err = filepath.Match(pattern, fi.Name())
			if err != nil {
				return err
			}

			if matched {
				read, err := ioutil.ReadFile(path)
				if err != nil {
					return err
				}

				if !silent {
					fmt.Println("Refactoring:", path)
				}

				newContents := strings.Replace(string(read), old, new, -1)

				err = os.WriteFile(path, []byte(newContents), 0)
				if err != nil {
					return err
				}
			}
		}

		return nil
	})
}
