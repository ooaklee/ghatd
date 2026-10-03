package router_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

// TestStrongETagPolicies covers the standard HTTP and stricter host profiles
// without silently changing either profile's length or byte grammar.
func TestStrongETagPolicies(t *testing.T) {
	t.Parallel()
	for _, profile := range []struct {
		name   string
		policy router.StrongETagPolicy
	}{
		{"default", router.StrongETagPolicy{}},
		{"HTTP trimmed", router.StrongETagPolicy{MaxBytes: 256, TrimSpace: true}},
		{"strict ASCII", router.StrongETagPolicy{MaxBytes: 512, ASCIIOnly: true}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			t.Parallel()
			maximum := profile.policy.MaxBytes
			if maximum == 0 {
				maximum = 256
			}
			for _, tc := range []struct {
				name, value string
				valid       bool
			}{
				{"minimal", `"a"`, true},
				{"opaque punctuation", `"a:1,_-!"`, true},
				{"empty tag", `""`, false},
				{"empty value", "", false},
				{"weak", `W/"a"`, false},
				{"wildcard", "*", false},
				{"list", `"a", "b"`, false},
				{"unquoted", "a", false},
				{"outer whitespace", " \t\"a\" \r\n", profile.policy.TrimSpace},
				{"embedded space", `"a b"`, false},
				{"embedded tab", "\"a\tb\"", false},
				{"embedded CRLF", "\"a\r\nb\"", false},
				{"embedded NUL", "\"a\x00b\"", false},
				{"delete byte", "\"a\x7fb\"", false},
				{"embedded quote", `"a"b"`, false},
				{"obs text", "\"a\xffb\"", !profile.policy.ASCIIOnly},
				{"UTF8 bytes", `"é"`, !profile.policy.ASCIIOnly},
				{"exact limit", `"` + strings.Repeat("a", maximum-2) + `"`, true},
				{"above limit", `"` + strings.Repeat("a", maximum-1) + `"`, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					err := profile.policy.Validate(tc.value)
					if tc.valid {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, router.ErrRouteInvalidRevision)
					}
				})
			}
		})
	}
}

// TestIfMatchHeader distinguishes optional absence from malformed supplied
// values and validates configuration even when no request proof was supplied.
func TestIfMatchHeader(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		values     []string
		required   bool
		nilRequest bool
		policy     router.StrongETagPolicy
		want       string
		wantError  error
	}{
		{name: "optional absent"},
		{name: "required absent", required: true, wantError: router.ErrRoutePrecondition},
		{name: "empty is not absent", values: []string{""}, wantError: router.ErrRouteInvalidRevision},
		{name: "duplicate", values: []string{`"a"`, `"a"`}, wantError: router.ErrRouteInvalidRevision},
		{name: "strong required", values: []string{`"a"`}, required: true, want: `"a"`},
		{name: "strong optional", values: []string{`"a"`}, want: `"a"`},
		{name: "normalize explicit", values: []string{`  "a"  `}, policy: router.StrongETagPolicy{TrimSpace: true}, want: `"a"`},
		{name: "strict whitespace", values: []string{`  "a"  `}, wantError: router.ErrRouteInvalidRevision},
		{name: "weak optional", values: []string{`W/"a"`}, wantError: router.ErrRouteInvalidRevision},
		{name: "nil request", nilRequest: true, wantError: router.ErrRouteInvalidRevision},
		{name: "invalid limit", values: []string{`"a"`}, policy: router.StrongETagPolicy{MaxBytes: 2}, wantError: router.ErrRouteConfiguration},
		{name: "negative limit", policy: router.StrongETagPolicy{MaxBytes: -1}, wantError: router.ErrRouteConfiguration},
		{name: "invalid absent optional", policy: router.StrongETagPolicy{MaxBytes: 1}, wantError: router.ErrRouteConfiguration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest("PATCH", "/items/one", nil)
			for _, value := range tc.values {
				r.Header.Add("If-Match", value)
			}
			if tc.nilRequest {
				r = nil
			}
			value, err := tc.policy.IfMatch(r, tc.required)
			require.ErrorIs(t, err, tc.wantError)
			require.Equal(t, tc.want, value)
		})
	}
}
