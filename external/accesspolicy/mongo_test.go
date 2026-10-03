package accesspolicy

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// policyFixture returns an independent bounded grant for each test case.
func policyFixture() Grant {
	return Grant{Subject: Subject{System: "sample", Kind: UserSubject, ID: "user-1"}, Revision: 1, Enabled: true,
		Scopes: []string{"items:write"}, Permissions: []string{"items:create"}, Tokens: TokenLimits{Permanent: 3},
		Limits: map[string]Limit{"api.requests": {Maximum: 3, WindowSeconds: 60}}}
}

func TestPolicyIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"opaque punctuation", "service:user.$id/part", true},
		{"utf8", "é", true},
		{"empty", "", false},
		{"whitespace", "a b", false},
		{"control", "a\x00b", false},
		{"invalid utf8", string([]byte{0xff}), false},
		{"maximum bytes", strings.Repeat("a", 256), true},
		{"over maximum", strings.Repeat("a", 257), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.valid, validName(tc.value)) })
	}
}

func TestGrantStorageEncoding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt string
	}{
		{"safe named-pair BSON", ""}, {"duplicate metric", "duplicate"}, {"wrong identity hash", "id"}, {"zero revision", "revision"}, {"invalid stored limit", "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := policyFixture()
			original.Limits["$metric.with.dots"] = Limit{Maximum: 4, WindowSeconds: 3600}
			record := encodeGrant(original)
			switch tc.corrupt {
			case "duplicate":
				record.Limits = append(record.Limits, record.Limits[0])
			case "id":
				record.ID = "other"
			case "revision":
				record.Revision = 0
			case "limit":
				record.Limits[0].Limit.Maximum = -1
			}
			data, err := bson.Marshal(record)
			require.NoError(t, err)
			require.Equal(t, bson.TypeArray, bson.Raw(data).Lookup("limits").Type)
			var decoded mongoGrant
			require.NoError(t, bson.Unmarshal(data, &decoded))
			grant, err := decoded.decode()
			if tc.corrupt != "" {
				require.ErrorIs(t, err, ErrConfiguration)
				return
			}
			require.NoError(t, err)
			require.Equal(t, original, grant)
			grant.Scopes[0] = "mutated"
			grant.Limits["api.requests"] = Limit{}
			require.Equal(t, "items:write", original.Scopes[0])
			require.Equal(t, int64(3), original.Limits["api.requests"].Maximum)
		})
	}
}

func TestTupleIdentity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		left, right []string
	}{
		{"delimiter placement", []string{"a:b", "c"}, []string{"a", "b:c"}},
		{"identity kind", []string{"s", "user", "id"}, []string{"s", "api_token", "id"}},
		{"system", []string{"s", "user", "id"}, []string{"other", "user", "id"}},
		{"byte-exact unicode", []string{"é"}, []string{"e\u0301"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, tupleID(tc.left...), tupleID(tc.right...))
			require.Equal(t, tupleID(tc.left...), tupleID(tc.left...))
		})
	}
}

func TestFixedWindow(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		unix, seconds, start, end int64
	}{
		{"inside", 125, 60, 120, 180}, {"boundary", 180, 60, 180, 240},
		{"pre epoch", -1, 60, -60, 0}, {"pre epoch boundary", -60, 60, -60, 0},
		{"single second", 125, 1, 125, 126},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end := fixedWindow(time.Unix(tc.unix, 999).In(time.FixedZone("offset", 3600)), tc.seconds)
			require.Equal(t, time.Unix(tc.start, 0).UTC(), start)
			require.Equal(t, time.Unix(tc.end, 0).UTC(), end)
		})
	}
}
