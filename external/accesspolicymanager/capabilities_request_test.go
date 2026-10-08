package accesspolicymanager

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const completeCapabilities = `{"enabled":true,"expires_at":null,"scopes":["partners.self"],"permissions":["partner.self"]}`

func TestCapabilityRequestStrictFields(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, encoding string
		valid                             bool
	}{
		{"complete explicit fields", completeCapabilities, "application/json", "", true},
		{"explicit empty authority", `{"enabled":false,"expires_at":null,"scopes":[],"permissions":[]}`, "application/json", "", true},
		{"explicit expiry offset", strings.Replace(completeCapabilities, "null", `"2026-10-08T15:00:00.123456+01:00"`, 1), "application/json", "", true},
		{"missing enabled", strings.Replace(completeCapabilities, `"enabled":true,`, "", 1), "application/json", "", false},
		{"missing expiry", strings.Replace(completeCapabilities, `"expires_at":null,`, "", 1), "application/json", "", false},
		{"null enabled", strings.Replace(completeCapabilities, `true`, `null`, 1), "application/json", "", false},
		{"null scopes", strings.Replace(completeCapabilities, `["partners.self"]`, `null`, 1), "application/json", "", false},
		{"null permissions", strings.Replace(completeCapabilities, `["partner.self"]`, `null`, 1), "application/json", "", false},
		{"duplicate escaped key", strings.Replace(completeCapabilities, `"enabled":true,`, `"enabled":true,"enabl\u0065d":false,`, 1), "application/json", "", false},
		{"unknown actor field", strings.Replace(completeCapabilities, `{"enabled"`, `{"actor_id":"admin","enabled"`, 1), "application/json", "", false},
		{"token allowances forbidden", strings.Replace(completeCapabilities, `{"enabled"`, `{"tokens":{"permanent":100},"enabled"`, 1), "application/json", "", false},
		{"wildcard permission", strings.Replace(completeCapabilities, "partner.self", "*", 1), "application/json", "", false},
		{"duplicate permission", strings.Replace(completeCapabilities, `["partner.self"]`, `["partner.self","partner.self"]`, 1), "application/json", "", false},
		{"invalid expiry", strings.Replace(completeCapabilities, "null", `"tomorrow"`, 1), "application/json", "", false},
		{"trailing object", completeCapabilities + `{}`, "application/json", "", false},
		{"non json media", completeCapabilities, "text/plain", "", false},
		{"encoded body", completeCapabilities, "application/json", "gzip", false},
		{"oversized body", completeCapabilities + strings.Repeat(" ", 131073), "application/json", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			if tc.encoding != "" {
				r.Header.Set("Content-Encoding", tc.encoding)
			}
			patch, err := readCapabilities(r)
			if !tc.valid {
				require.ErrorIs(t, err, ErrInvalidRequest)
				require.Zero(t, patch)
				return
			}
			require.NoError(t, err)
			if tc.name == "explicit expiry offset" {
				require.Equal(t, time.Date(2026, 10, 8, 14, 0, 0, 123000000, time.UTC), patch.ExpiresAt)
			}
		})
	}
}

func TestCapabilityRequestArrayBounds(t *testing.T) {
	for _, size := range []int{128, 129} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			names := make([]string, size)
			for i := range names {
				names[i] = "scope-" + strings.Repeat("x", i+1)
			}
			body, err := json.Marshal(map[string]any{"enabled": true, "expires_at": nil, "scopes": names, "permissions": []string{}})
			require.NoError(t, err)
			r := httptest.NewRequest("PUT", "/", strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			_, err = readCapabilities(r)
			if size == 128 {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidRequest)
			}
		})
	}
}
