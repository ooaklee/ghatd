package accesspolicymanager

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/ooaklee/ghatd/external/accesspolicy"
)

// readLimits accepts exactly five explicit integer fields. Duplicate, unknown,
// null, fractional and trailing values are rejected rather than silently merged.
// The policy domain validates ranges/coherence before any inventory preparation.
func readLimits(r *http.Request) (accesspolicy.TokenLimits, error) {
	var result accesspolicy.TokenLimits
	if r == nil || r.Body == nil || len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("Content-Encoding")) > 1 || r.Header.Get("Content-Encoding") != "" {
		return result, ErrInvalidRequest
	}
	media, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (parameters["charset"] != "" && !strings.EqualFold(parameters["charset"], "utf-8")) {
		return result, ErrInvalidRequest
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(body) > 4096 {
		return result, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return result, ErrInvalidRequest
	}
	fields := map[string]*int64{"permanent": &result.Permanent, "ephemeral": &result.Ephemeral, "minimum_ttl": &result.MinimumTTL, "maximum_ttl": &result.MaximumTTL, "ttl_increment": &result.TTLIncrement}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return accesspolicy.TokenLimits{}, ErrInvalidRequest
		}
		name, ok := key.(string)
		field := fields[name]
		if !ok || field == nil {
			return accesspolicy.TokenLimits{}, ErrInvalidRequest
		}
		var value *int64
		if err := decoder.Decode(&value); err != nil || value == nil {
			return accesspolicy.TokenLimits{}, ErrInvalidRequest
		}
		*field = *value
		delete(fields, name)
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(fields) != 0 {
		return accesspolicy.TokenLimits{}, ErrInvalidRequest
	}
	if _, err = decoder.Token(); err != io.EOF {
		return accesspolicy.TokenLimits{}, ErrInvalidRequest
	}
	return result, nil
}

// readRevision accepts one canonical decimal strong tag, including "0" for
// create-only. Weak tags, wildcards, lists, signs and ambiguous leading zeros fail.
func readRevision(r *http.Request) (int64, error) {
	if r == nil {
		return 0, ErrInvalidRequest
	}
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		return 0, ErrPreconditionRequired
	}
	if len(values) != 1 || len(values[0]) < 3 || len(values[0]) > 18 {
		return 0, ErrInvalidRequest
	}
	value := values[0]
	if value[0] != '"' || value[len(value)-1] != '"' {
		return 0, ErrInvalidRequest
	}
	revision, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil || revision < 0 || revision > 9007199254740991 || revisionTag(revision) != value {
		return 0, ErrInvalidRequest
	}
	return revision, nil
}
