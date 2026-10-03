package accesspolicy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrantValidation(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		value       int64
		valid       bool
	}{
		{"valid", "", 0, true},
		{"empty system", "system", 0, false}, {"invalid subject kind", "kind", 0, false},
		{"wildcard scope", "scope", 0, false}, {"wildcard permission", "permission", 0, false},
		{"too many scopes", "scopes", 129, false}, {"too many permissions", "permissions", 129, false},
		{"negative permanent inventory", "permanent", -1, false}, {"excessive permanent inventory", "permanent", 1000001, false},
		{"negative ephemeral inventory", "ephemeral", -1, false},
		{"ephemeral needs TTL", "ephemeral", 1, false}, {"valid ephemeral TTL", "ephemeral-valid", 1, true},
		{"coherent dormant TTL", "ephemeral-valid", 0, true},
		{"contradictory dormant TTL", "dormant-invalid", 0, false},
		{"no permitted step in range", "no-step", 0, false},
		{"negative quota", "quota", -1, false}, {"zero quota explicitly disables", "quota", 0, true},
		{"oversized quota", "quota", 1000000001, false},
		{"zero window", "window", 0, false}, {"negative window", "window", -1, false},
		{"one year window", "window", 31536000, true}, {"excessive window", "window", 31536001, false},
		{"empty metric", "metric", 0, false}, {"too many metrics", "metrics", 65, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grant := policyFixture()
			switch tc.field {
			case "system":
				grant.Subject.System = ""
			case "kind":
				grant.Subject.Kind = "other"
			case "scope":
				grant.Scopes = []string{"*"}
			case "permission":
				grant.Permissions = []string{"items:*"}
			case "scopes":
				grant.Scopes = make([]string, tc.value)
			case "permissions":
				grant.Permissions = make([]string, tc.value)
			case "permanent":
				grant.Tokens.Permanent = tc.value
			case "ephemeral":
				grant.Tokens.Ephemeral = tc.value
			case "ephemeral-valid":
				grant.Tokens = TokenLimits{Ephemeral: tc.value, MinimumTTL: 60, MaximumTTL: 3600, TTLIncrement: 60}
			case "dormant-invalid":
				grant.Tokens = TokenLimits{MinimumTTL: 100, MaximumTTL: 60, TTLIncrement: 10}
			case "no-step":
				grant.Tokens = TokenLimits{Ephemeral: 1, MinimumTTL: 61, MaximumTTL: 100, TTLIncrement: 60}
			case "quota":
				grant.Limits["api.requests"] = Limit{Maximum: tc.value, WindowSeconds: 60}
			case "window":
				grant.Limits["api.requests"] = Limit{Maximum: 3, WindowSeconds: tc.value}
			case "metric":
				grant.Limits[""] = Limit{Maximum: 3, WindowSeconds: 60}
			case "metrics":
				for i := int64(0); i < tc.value; i++ {
					grant.Limits[strings.Repeat("m", int(i)+1)] = Limit{Maximum: 1, WindowSeconds: 1}
				}
			}
			if tc.valid {
				require.NoError(t, validateGrant(grant))
			} else {
				require.ErrorIs(t, validateGrant(grant), ErrConfiguration)
			}
		})
	}
}

func TestConsumptionValidation(t *testing.T) {
	for _, tc := range []struct {
		name, field, value string
		valid              bool
	}{
		{"valid", "", "", true}, {"invalid identity kind", "kind", "admin", false},
		{"empty metric", "metric", "", false}, {"empty operation", "key", "", false},
		{"empty fingerprint", "fingerprint", "", false}, {"oversized operation", "key", strings.Repeat("x", 257), false},
		{"wildcard scope", "scope", "items:*", false}, {"whitespace permission", "permission", "items read", false},
		{"oversized scopes", "scopes", "", false}, {"oversized permissions", "permissions", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := consumptionFixture()
			switch tc.field {
			case "kind":
				request.Subject.Kind = tc.value
			case "metric":
				request.Metric = tc.value
			case "key":
				request.Key = tc.value
			case "fingerprint":
				request.Fingerprint = tc.value
			case "scope":
				request.Scopes = []string{tc.value}
			case "permission":
				request.Permissions = []string{tc.value}
			case "scopes":
				request.Scopes = make([]string, 129)
			case "permissions":
				request.Permissions = make([]string, 129)
			}
			if tc.valid {
				require.NoError(t, validateConsumption(request))
			} else {
				require.ErrorIs(t, validateConsumption(request), ErrConfiguration)
			}
		})
	}
}

func TestUninitializedStoreFailsClosed(t *testing.T) {
	for _, operation := range []string{"read", "replace", "grant", "consume", "business"} {
		t.Run(operation, func(t *testing.T) {
			store, ctx := &MongoStore{}, context.Background()
			var err error
			called := false
			switch operation {
			case "read":
				_, err = store.Read(ctx, policyFixture().Subject)
			case "replace":
				_, err = store.Replace(ctx, policyFixture(), 0, "admin", time.Now())
			case "grant":
				err = store.WithGrant(ctx, policyFixture().Subject, time.Now(), func(context.Context, Grant) error { called = true; return nil })
			case "consume":
				_, err = store.Consume(ctx, consumptionFixture(), time.Now())
			case "business":
				_, err = store.WithConsumption(ctx, consumptionFixture(), time.Now(), ConsumptionAction{Check: func(context.Context) error { called = true; return nil }, Apply: func(context.Context, Usage) error { called = true; return nil }})
			}
			require.ErrorIs(t, err, ErrConfiguration)
			require.False(t, called)
		})
	}
}
