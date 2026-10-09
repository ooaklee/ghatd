package partnerhttp

import (
	"encoding/json"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestServiceConfigurationAndStrictDTOs(t *testing.T) {
	for _, tc := range []struct {
		name, origin, path string
		local, valid       bool
	}{
		{"https", "https://host.example.test", "/ref", false, true}, {"default_path", "https://host.example.test", "", false, true},
		{"alternate_prefix", "https://host.example.test", "/referrals/join", false, true},
		{"explicit_local", "http://127.0.0.1:4000", "/ref", true, true}, {"local_not_opted_in", "http://127.0.0.1:4000", "/ref", false, false},
		{"remote_http", "http://host.example.test", "/ref", true, false}, {"origin_path", "https://host.example.test/app", "/ref", false, false},
		{"origin_query", "https://host.example.test?private=1", "/ref", false, false}, {"userinfo", "https://private@host.example.test", "/ref", false, false},
		{"wildcard", "https://*.example.test", "/ref", false, false}, {"relative_prefix", "https://host.example.test", "ref", false, false},
		{"root_prefix", "https://host.example.test", "/", false, false}, {"traversal", "https://host.example.test", "/ref/../other", false, false},
		{"query_prefix", "https://host.example.test", "/ref?x=1", false, false}, {"external_prefix", "https://host.example.test", "//other.example.test", false, false},
		{"escaped_prefix", "https://host.example.test", "/ref%2Fother", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, _, sessions, _, _ := operatorAccessFixture(t, partnermanager.CapabilityPolicy, partneraccess.ProgramScope)
			_, err := NewService(ServiceConfig{Manager: &partnermanager.Manager{}, Sessions: sessions, Authority: fixture.authority, PublicOrigin: tc.origin, ReferralPath: tc.path, AllowLocalHTTP: tc.local})
			if tc.valid {
				require.NoError(t, err)
			} else {
				requireOperatorAccessError(t, err, 500)
			}
		})
	}
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"object", `{"value":"allowed"}`, true}, {"empty", `{}`, true}, {"unknown_actor", `{"value":"allowed","actor":"forged"}`, false}, {"trailing", `{} {}`, false}, {"wrong_type", `{"value":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dto struct {
				Value string `json:"value"`
			}
			err := decode(json.RawMessage(tc.body), &dto)
			if tc.valid {
				require.NoError(t, err)
			} else {
				requireOperatorAccessError(t, err, 400)
			}
		})
	}
}
