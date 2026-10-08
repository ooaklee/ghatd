package accesspolicymanager

import (
	"bytes"
	"encoding/json"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// readCapabilities requires all four explicit fields and bounds the entire
// request. Duplicate/unknown/null authority fields and trailing JSON fail closed.
// Null expiry explicitly removes expiry; arrays must be present, even if empty.
func readCapabilities(r *http.Request) (accesspolicy.Capabilities, error) {
	var patch accesspolicy.Capabilities
	if r == nil || r.Body == nil || len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("Content-Encoding")) > 1 || r.Header.Get("Content-Encoding") != "" {
		return patch, ErrInvalidRequest
	}
	media, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (parameters["charset"] != "" && !strings.EqualFold(parameters["charset"], "utf-8")) {
		return patch, ErrInvalidRequest
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 131073))
	if err != nil || len(body) > 131072 {
		return patch, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return patch, ErrInvalidRequest
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		key, e := decoder.Token()
		name, ok := key.(string)
		if e != nil || !ok {
			return accesspolicy.Capabilities{}, ErrInvalidRequest
		}
		if _, duplicate := fields[name]; duplicate {
			return accesspolicy.Capabilities{}, ErrInvalidRequest
		}
		switch name {
		case "enabled", "expires_at", "scopes", "permissions":
		default:
			return accesspolicy.Capabilities{}, ErrInvalidRequest
		}
		var value json.RawMessage
		if e = decoder.Decode(&value); e != nil {
			return accesspolicy.Capabilities{}, ErrInvalidRequest
		}
		fields[name] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(fields) != 4 {
		return accesspolicy.Capabilities{}, ErrInvalidRequest
	}
	if _, err = decoder.Token(); err != io.EOF {
		return accesspolicy.Capabilities{}, ErrInvalidRequest
	}
	for _, name := range []string{"enabled", "scopes", "permissions"} {
		if bytes.Equal(fields[name], []byte("null")) {
			return accesspolicy.Capabilities{}, ErrInvalidRequest
		}
	}
	if json.Unmarshal(fields["enabled"], &patch.Enabled) != nil || json.Unmarshal(fields["scopes"], &patch.Scopes) != nil || json.Unmarshal(fields["permissions"], &patch.Permissions) != nil {
		return accesspolicy.Capabilities{}, ErrInvalidRequest
	}
	if !bytes.Equal(fields["expires_at"], []byte("null")) {
		var value string
		if json.Unmarshal(fields["expires_at"], &value) != nil || value == "" {
			return accesspolicy.Capabilities{}, ErrInvalidRequest
		}
		patch.ExpiresAt, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return accesspolicy.Capabilities{}, ErrInvalidRequest
		}
		patch.ExpiresAt = patch.ExpiresAt.UTC().Truncate(time.Millisecond)
	}
	if accesspolicy.ValidateCapabilities(patch) != nil {
		return accesspolicy.Capabilities{}, ErrInvalidRequest
	}
	return patch, nil
}
