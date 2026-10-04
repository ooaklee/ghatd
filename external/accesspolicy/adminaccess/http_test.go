package adminaccess

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeCodec(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, encoding string
		valid                             bool
	}{
		{"canonical", `{"code":"012345ABCDEF"}`, "application/json", "", true},
		{"duplicate", `{"code":"012345ABCDEF","code":"012345ABCDEF"}`, "application/json", "", false},
		{"unknown", `{"code":"012345ABCDEF","role":"ADMIN"}`, "application/json", "", false},
		{"lowercase", `{"code":"012345abcdef"}`, "application/json", "", false},
		{"short", `{"code":"01234567"}`, "application/json", "", false},
		{"null", `{"code":null}`, "application/json", "", false},
		{"trailing", `{"code":"012345ABCDEF"}{}`, "application/json", "", false},
		{"oversized", `{"code":"012345ABCDEF"}` + strings.Repeat(" ", 130), "application/json", "", false},
		{"wrong type", `{"code":"012345ABCDEF"}`, "text/plain", "", false},
		{"compressed", `{"code":"012345ABCDEF"}`, "application/json", "gzip", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			if tc.encoding != "" {
				r.Header.Set("Content-Encoding", tc.encoding)
			}
			code, err := readCode(r)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "012345ABCDEF", code)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSecurityHeaderCardinality(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		want   string
	}{
		{"absent", nil, ""}, {"single", []string{"1"}, "1"}, {"duplicate", []string{"1", "1"}, ""}, {"combined", []string{"1,1"}, "1,1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			for _, v := range tc.values {
				r.Header.Add("X-Admin-Access", v)
			}
			require.Equal(t, tc.want, one(r, "X-Admin-Access"))
		})
	}
}
