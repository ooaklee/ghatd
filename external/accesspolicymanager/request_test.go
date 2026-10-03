package accesspolicymanager

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/stretchr/testify/require"
)

const completeLimits = `{"permanent":2,"ephemeral":1,"minimum_ttl":60,"maximum_ttl":3600,"ttl_increment":60}`

func TestReadLimits(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, encoding string
		duplicateType, valid              bool
	}{
		{"complete", completeLimits, "application/json", "", false, true},
		{"UTF8", completeLimits, "application/json; charset=UTF-8", "", false, true},
		{"whitespace", " \n" + completeLimits + "\n", "application/json", "", false, true},
		{"missing type", completeLimits, "", "", false, false},
		{"wrong type", completeLimits, "text/plain", "", false, false},
		{"duplicate type", completeLimits, "application/json", "", true, false},
		{"unsupported charset", completeLimits, "application/json; charset=utf-16", "", false, false},
		{"compressed", completeLimits, "application/json", "gzip", false, false},
		{"empty", "", "application/json", "", false, false},
		{"null object", "null", "application/json", "", false, false},
		{"array", "[]", "application/json", "", false, false},
		{"missing fields", `{"permanent":2}`, "application/json", "", false, false},
		{"duplicate field", strings.Replace(completeLimits, `"permanent":2`, `"permanent":1,"permanent":2`, 1), "application/json", "", false, false},
		{"unknown field", strings.Replace(completeLimits, `"permanent":2`, `"actor":"admin","permanent":2`, 1), "application/json", "", false, false},
		{"null integer", strings.Replace(completeLimits, `"permanent":2`, `"permanent":null`, 1), "application/json", "", false, false},
		{"fraction", strings.Replace(completeLimits, `"permanent":2`, `"permanent":2.0`, 1), "application/json", "", false, false},
		{"string", strings.Replace(completeLimits, `"permanent":2`, `"permanent":"2"`, 1), "application/json", "", false, false},
		{"overflow", strings.Replace(completeLimits, `"permanent":2`, `"permanent":9223372036854775808`, 1), "application/json", "", false, false},
		{"trailing object", completeLimits + "{}", "application/json", "", false, false},
		{"trailing junk", completeLimits + "!", "application/json", "", false, false},
		{"truncated", completeLimits[:len(completeLimits)-1], "application/json", "", false, false},
		{"oversized", completeLimits + strings.Repeat(" ", 4096), "application/json", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			if tc.duplicateType {
				r.Header.Add("Content-Type", tc.contentType)
			}
			r.Header.Set("Content-Encoding", tc.encoding)
			got, err := readLimits(r)
			if !tc.valid {
				require.ErrorIs(t, err, ErrInvalidRequest)
				return
			}
			require.NoError(t, err)
			require.Equal(t, accesspolicy.TokenLimits{Permanent: 2, Ephemeral: 1, MinimumTTL: 60, MaximumTTL: 3600, TTLIncrement: 60}, got)
		})
	}
}

func TestReadRevision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   []string
		expected int64
		err      error
	}{
		{"absent", nil, 0, ErrPreconditionRequired},
		{"create", []string{`"0"`}, 0, nil},
		{"update", []string{`"42"`}, 42, nil},
		{"maximum", []string{`"9007199254740991"`}, 9007199254740991, nil},
		{"too large", []string{`"9007199254740992"`}, 0, ErrInvalidRequest},
		{"negative", []string{`"-1"`}, 0, ErrInvalidRequest},
		{"sign", []string{`"+1"`}, 0, ErrInvalidRequest},
		{"leading zero", []string{`"01"`}, 0, ErrInvalidRequest},
		{"weak", []string{`W/"1"`}, 0, ErrInvalidRequest},
		{"wildcard", []string{"*"}, 0, ErrInvalidRequest},
		{"list", []string{`"1", "2"`}, 0, ErrInvalidRequest},
		{"multiple", []string{`"1"`, `"2"`}, 0, ErrInvalidRequest},
		{"empty", []string{""}, 0, ErrInvalidRequest},
		{"unquoted", []string{"1"}, 0, ErrInvalidRequest},
		{"outer spaces", []string{` "1" `}, 0, ErrInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "/", nil)
			for _, value := range tc.values {
				r.Header.Add("If-Match", value)
			}
			got, err := readRevision(r)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestManagementRequestEdgeCases(t *testing.T) {
	for _, variant := range []string{"nil limits request", "nil revision request", "duplicate encoding"} {
		t.Run(variant, func(t *testing.T) {
			var err error
			switch variant {
			case "nil limits request":
				_, err = readLimits(nil)
			case "nil revision request":
				_, err = readRevision(nil)
			case "duplicate encoding":
				r := httptest.NewRequest("POST", "/", strings.NewReader(completeLimits))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Add("Content-Encoding", "")
				r.Header.Add("Content-Encoding", "gzip")
				_, err = readLimits(r)
			}
			require.ErrorIs(t, err, ErrInvalidRequest)
		})
	}
}
