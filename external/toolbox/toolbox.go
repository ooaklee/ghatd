package toolbox

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/ooaklee/ghatd/external/common"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const ()

type (
	// BaseValidator is a base validator
	BaseValidator interface {
		Validate(request interface{}) error
	}
)

// ValidateParsedRequest validates based on tags. On failure an error is returned
func ValidateParsedRequest(request interface{}, validator BaseValidator) error {
	return validator.Validate(request)
}

// TimeNowUTC returns time as string in RFC3339 format without
// the timezone
func TimeNowUTC() string {
	return time.Now().UTC().Format(common.RFC3339NanoUTC)
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

// StringInSlice checks to see if string is within slice
func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// StringStandardisedToUpper returns a string with no explicit spacing strategy
// that is all uppercase and standardised.
func StringStandardisedToUpper(s string) string {
	s = strings.ToUpper(s)

	return StringRemoveMultiSpace(strings.TrimSpace(strings.ReplaceAll(s, "’", "'")))
}

// StringStandardisedToLower returns a string with no explicit spacing strategy
// that is all lowercase and standardised.
func StringStandardisedToLower(s string) string {
	s = strings.ToLower(s)

	return StringRemoveMultiSpace(strings.TrimSpace(strings.ReplaceAll(s, "’", "'")))
}

// StringConvertToSnakeCase subsitutes all instances of a space with an underscore
func StringConvertToSnakeCase(s string) string {

	s = StringRemoveMultiSpace(s)

	return strings.Replace(s, " ", "_", -1)
}

// StringConvertToKebabCase returns a string in kebab case format
func StringConvertToKebabCase(text string) (string, error) {

	// Trim string
	text = strings.TrimSpace(text)

	// Remove all the special characters
	reg, err := regexp.Compile(`[^a-zA-Z0-9\\s]+`)
	if err != nil {
		return "", err
	}
	cleanedText := reg.ReplaceAllString(text, " ")

	// Make sure it's lower case and remove double space
	cleanedText = strings.ToLower(
		StringRemoveMultiSpace(
			cleanedText,
		),
	)

	// Remove Spaces for hyphen
	cleanedText = strings.ReplaceAll(cleanedText, " ", "-")

	return cleanedText, nil
}

// StringConvertToTitleCase returns a string in title case format
func StringConvertToTitleCase(text string) string {
	s := StringRemoveMultiSpace(strings.TrimSpace(text))

	caser := cases.Title(language.English)
	stringAsTitle := caser.String(s)

	return stringAsTitle
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

// AddStringIfItDoesExistInBaseString merges an additional passed string into
// the provided base string only if the additional string doesn't already exist
// in the base string. The additional string must be a space-separated string.
func AddStringIfItDoesExistInBaseString(baseString, additionalString string) string {
	var additionalValidStrings string
	var splitExtraString []string = strings.Split(additionalString, " ")

	for _, str := range splitExtraString {
		if !strings.Contains(baseString, str) {
			additionalValidStrings += (" " + str)
		}
	}

	return baseString + additionalValidStrings
}

// GetIfEnvOrDefault handles checking if an environment
// variable is set, or default to other passed string
func GetIfEnvOrDefault(envName, defaultValue string) string {

	if foundValue := os.Getenv(strings.ToUpper(envName)); foundValue != "" {
		return foundValue
	}

	return defaultValue
}

// ConvertToBoolean if the string is 1 or true, convert to true.
// Otherwise, set to false.
func ConvertToBoolean(s string) bool {
	for _, v := range []string{"1", "true"} {
		if strings.EqualFold(s, v) {
			return true
		}
	}

	return false
}

// RemoveStringFromSlice checks to see if string is within slice and removes it, returning new slice
func RemoveStringFromSlice(a string, list []string) []string {
	var removeIndex int

	for i, text := range list {
		if text == a {
			removeIndex = i
		}
	}

	return removeElement(list, removeIndex)
}

// removeElement delete element at passed index from passed list
func removeElement(col []string, removeIndex int) []string {
	col[removeIndex] = col[len(col)-1]
	return col[:len(col)-1]
}

// ConvertStringToIntOrDefault attempts to turn passed number (as string) to int, on
// failure the default value will be used
func ConvertStringToIntOrDefault(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return defaultValue
	}

	return int(f)
}

// GetStaticUserUuid returns the UUID used for unauthed user access into service
func GetStaticUserUuid() string {
	return "38adc27a-a666-401d-8309-613273c2cb60"
}

// AddRedirectHeaderTo handles the logic of adding the redirect header to the response and
// forwarding user to the target url.
func AddRedirectHeaderTo(w http.ResponseWriter, r *http.Request, targetUrl string) {
	if contentTypeHeader := r.Header["Content-Type"]; len(contentTypeHeader) == 0 || contentTypeHeader[0] != "application/json" {

		w.Header().Add("Location", targetUrl)
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	}
}

// AddNonSecureAuthInfoCookie is handling adding the cookies to the response that hold information
// about the security token cookies
func AddNonSecureAuthInfoCookie(w http.ResponseWriter, cookieDomain, environment string, accessTokenExpiresAt int64, refressTokenExpiresAt int64) {

	//accesstoken cookie
	AddNonSecureCookie(w, environment, common.AccessTokenAuthInfoCookieName, "true", cookieDomain, accessTokenExpiresAt)

	//refreshtoken cookie
	AddNonSecureCookie(w, environment, common.RefreshTokenAuthInfoCookieName, "true", cookieDomain, refressTokenExpiresAt)
}

// AddNonSecureCookie adds a non-secure cookie to the response.
// It sets the cookie name, value, domain and expiration time. The environment
// parameter is used to determine if the cookie should be secure or not.
func AddNonSecureCookie(w http.ResponseWriter, environment, cookieName, cookieValue, cookieDomain string, cookieExpiresAt int64) {

	nonSecureCookie := http.Cookie{
		Name:    cookieName,
		Value:   cookieValue,
		Domain:  cookieDomain,
		Path:    "/",
		Expires: time.Unix(cookieExpiresAt, 0),
		Secure: func(env string) bool {
			return env != "local"
		}(environment),
		SameSite: func(env string) http.SameSite {

			if env != "local" {
				return http.SameSiteStrictMode
			}
			return http.SameSiteLaxMode

		}(environment),
	}

	http.SetCookie(w, &nonSecureCookie)
}

// RemoveCookiesWithName is handling removing the passed cookie from the client
func RemoveCookiesWithName(w http.ResponseWriter, environment, cookieName, cookieDomain string) {

	expirationNow := time.Now()

	removeReferencedCookie := http.Cookie{
		Name:    cookieName,
		Domain:  cookieDomain,
		Path:    "/",
		Expires: expirationNow,
		MaxAge:  -1,
		Secure: func(env string) bool {
			return env != "local"
		}(environment),
		HttpOnly: true,
		SameSite: func(env string) http.SameSite {

			if env != "local" {
				return http.SameSiteStrictMode
			}
			return http.SameSiteLaxMode

		}(environment),
	}

	http.SetCookie(w, &removeReferencedCookie)
}

// AddAuthCookies is handling adding the auth token cookies (access & refresh) to the response
func AddAuthCookies(w http.ResponseWriter, environment, cookieDomain, accessTokenCookiePrefix, accessToken string, accessTokenExpiresAt int64, refreshTokenCookiePrefix, refressToken string, refressTokenExpiresAt int64) {
	accessAuthcookie := http.Cookie{
		Name:    accessTokenCookiePrefix,
		Value:   accessToken,
		Domain:  cookieDomain,
		Path:    "/",
		Expires: time.Unix(accessTokenExpiresAt, 0),
		Secure: func(env string) bool {
			return env != "local"
		}(environment),
		HttpOnly: true,
		SameSite: func(env string) http.SameSite {

			if env != "local" {
				return http.SameSiteStrictMode
			}
			return http.SameSiteLaxMode

		}(environment),
	}

	// based on https://tkacz.pro/how-to-securely-store-jwt-tokens/
	refreshAuthcookie := http.Cookie{
		Name:    refreshTokenCookiePrefix,
		Value:   refressToken,
		Domain:  cookieDomain,
		Path:    "/",
		Expires: time.Unix(refressTokenExpiresAt, 0),
		Secure: func(env string) bool {
			return env != "local"
		}(environment),
		HttpOnly: true,
		SameSite: func(env string) http.SameSite {

			if env != "local" {
				return http.SameSiteStrictMode
			}
			return http.SameSiteLaxMode

		}(environment),
	}

	http.SetCookie(w, &accessAuthcookie)
	http.SetCookie(w, &refreshAuthcookie)
}

// RemoveAuthCookies is handling removing the auth token cookies (access & refresh) from the client
func RemoveAuthCookies(w http.ResponseWriter, environment, cookieDomain, accessTokenCookiePrefix, refressTokenExpiresAt string) {

	expirationNow := time.Now()

	removeAccessAuthcookie := http.Cookie{
		Name:    accessTokenCookiePrefix,
		Domain:  cookieDomain,
		Path:    "/",
		Expires: expirationNow,
		MaxAge:  -1,
		Secure: func(env string) bool {
			return env != "local"
		}(environment),
		HttpOnly: true,
		SameSite: func(env string) http.SameSite {

			if env != "local" {
				return http.SameSiteStrictMode
			}
			return http.SameSiteLaxMode

		}(environment),
	}

	// based on https://tkacz.pro/how-to-securely-store-jwt-tokens/
	removeRefreshAuthcookie := http.Cookie{
		Name:    refressTokenExpiresAt,
		Domain:  cookieDomain,
		Path:    "/",
		Expires: expirationNow,
		MaxAge:  -1,
		Secure: func(env string) bool {
			return env != "local"
		}(environment),
		HttpOnly: true,
		SameSite: func(env string) http.SameSite {

			if env != "local" {
				return http.SameSiteStrictMode
			}
			return http.SameSiteLaxMode

		}(environment),
	}

	http.SetCookie(w, &removeAccessAuthcookie)
	http.SetCookie(w, &removeRefreshAuthcookie)
}

// GenerateTimeOfExpiryAsSeconds calculates the duration to the Time of
// Expiry (ToE) in seconds by adding the provided duration to the current time.
func GenerateTimeOfExpiryAsSeconds(ttlDuration time.Duration) int64 {
	return time.Now().Add(ttlDuration).Unix()
}

// CombinedUuidFormat returns a string containing a combination of <userID>:<tokenUUID>
// a format that is used for referencing tokens in ephemeral storage.
func CombinedUuidFormat(userID, tokenUUID string) string {
	return fmt.Sprintf("%v:%v", userID, tokenUUID)
}
