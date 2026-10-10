package accesspolicy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDefaultAuthorityKeepsStoredRestrictions proves that only authoritative
// absence can activate host defaults, without writing or manufacturing grants.
func TestDefaultAuthorityKeepsStoredRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stored  bool
		mutate  func(*Grant)
		readErr error
		want    error
	}{
		{name: "known absence"},
		{name: "wrapped absence", readErr: fmt.Errorf("read: %w", ErrDenied)},
		{name: "explicit allowed grant", stored: true},
		{name: "revoked", stored: true, mutate: func(g *Grant) { g.Enabled = false }, want: ErrDenied},
		{name: "expired", stored: true, mutate: func(g *Grant) { g.ExpiresAt = time.Now().Add(-time.Second) }, want: ErrDenied},
		{name: "omitted permission", stored: true, mutate: func(g *Grant) { g.Permissions = nil }, want: ErrDenied},
		{name: "omitted scope", stored: true, mutate: func(g *Grant) { g.Scopes = nil }, want: ErrDenied},
		{name: "wrong subject", stored: true, mutate: func(g *Grant) { g.Subject.ID = "other" }, want: ErrDenied},
		{name: "corrupt grant", stored: true, mutate: func(g *Grant) { g.Scopes = []string{"*"} }, want: ErrConfiguration},
		{name: "outage", readErr: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "joined denial and outage", readErr: errors.Join(ErrDenied, context.DeadlineExceeded), want: context.DeadlineExceeded},
		{name: "joined single denial", readErr: errors.Join(ErrDenied), want: ErrDenied},
		{name: "denial alias", readErr: absenceAlias{}, want: ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grant := policyFixture()
			grant.Scopes, grant.Permissions = []string{"member.self"}, []string{"member.enroll"}
			subject := grant.Subject
			store := &migrationStore{readErr: tc.readErr}
			if tc.mutate != nil {
				tc.mutate(&grant)
			}
			if tc.stored {
				store.current = &grant
			}
			svc, err := NewService(store, nil)
			require.NoError(t, err)
			a, err := NewDefaultAuthorizer(svc, subject.System, []string{"member.self"}, []string{"member.enroll"})
			require.NoError(t, err)
			err = a.Authorize(context.Background(), subject, []string{"member.self"}, []string{"member.enroll"})
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			require.Zero(t, store.writes)
		})
	}
}

// TestDefaultAuthorityDoesNotBroadenIdentityOrCapabilities keeps token,
// operator, worker and cross-system requests outside ordinary-user defaults.
func TestDefaultAuthorityDoesNotBroadenIdentityOrCapabilities(t *testing.T) {
	for _, tc := range []struct{ name, kind, system, scope, permission string }{
		{"token", APITokenSubject, "sample", "member.self", "member.enroll"},
		{"other system", UserSubject, "other", "member.self", "member.enroll"},
		{"operator", UserSubject, "sample", "program", "policy.publish"},
		{"worker", UserSubject, "sample", "program", "revenue.process"},
		{"other resource", UserSubject, "sample", "member.other", "member.enroll"},
		{"missing scope", UserSubject, "sample", "", "member.enroll"},
		{"missing permission", UserSubject, "sample", "member.self", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &migrationStore{}
			svc, err := NewService(store, nil)
			require.NoError(t, err)
			scopes, permissions := []string{"member.self"}, []string{"member.enroll"}
			a, err := NewDefaultAuthorizer(svc, "sample", scopes, permissions)
			require.NoError(t, err)
			scopes[0], permissions[0] = tc.scope, tc.permission
			err = a.Authorize(context.Background(), Subject{System: tc.system, Kind: tc.kind, ID: "customer"}, []string{tc.scope}, []string{tc.permission})
			require.ErrorIs(t, err, ErrDenied)
			require.Zero(t, store.writes)
		})
	}
}

// TestDefaultAuthorityRechecksRevocation exercises a retained authorizer across
// an administrator's explicit revocation, rather than caching first admission.
func TestDefaultAuthorityRechecksRevocation(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		t.Run(fmt.Sprint("disabled=", disabled), func(t *testing.T) {
			store := &migrationStore{}
			svc, err := NewService(store, nil)
			require.NoError(t, err)
			a, err := NewDefaultAuthorizer(svc, "sample", []string{"member.self"}, []string{"member.enroll"})
			require.NoError(t, err)
			subject := Subject{System: "sample", Kind: UserSubject, ID: "customer"}
			require.NoError(t, a.Authorize(context.Background(), subject, []string{"member.self"}, []string{"member.enroll"}))
			store.current = &Grant{Subject: subject, Revision: 1, Enabled: !disabled, Scopes: []string{"member.self"}, Permissions: []string{"member.enroll"}}
			if !disabled {
				store.current.ExpiresAt = time.Now().Add(-time.Second)
			}
			require.ErrorIs(t, a.Authorize(context.Background(), subject, []string{"member.self"}, []string{"member.enroll"}), ErrDenied)
		})
	}
}

// TestDefaultAuthorityConfiguration rejects incomplete or wildcard defaults
// before the host can install an authorizer.
func TestDefaultAuthorityConfiguration(t *testing.T) {
	for _, variant := range []string{"nil service", "nil store", "empty system", "empty scopes", "empty permissions", "wildcard", "duplicate"} {
		t.Run(variant, func(t *testing.T) {
			service := &Service{store: &migrationStore{}}
			system, scopes, permissions := "sample", []string{"member.self"}, []string{"member.enroll"}
			switch variant {
			case "nil service":
				service = nil
			case "nil store":
				service.store = nil
			case "empty system":
				system = ""
			case "empty scopes":
				scopes = nil
			case "empty permissions":
				permissions = nil
			case "wildcard":
				permissions = []string{"*"}
			case "duplicate":
				scopes = []string{"member.self", "member.self"}
			}
			a, err := NewDefaultAuthorizer(service, system, scopes, permissions)
			require.ErrorIs(t, err, ErrConfiguration)
			require.Nil(t, a)
		})
	}
}

// TestDefaultAuthorityContext checks cancellation both before dispatch and
// after a storage implementation returns a grant despite cancellation.
func TestDefaultAuthorityContext(t *testing.T) {
	for _, variant := range []string{"nil authorizer", "nil context", "canceled before read", "canceled during read"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			store := &boundaryStore{}
			a, err := NewDefaultAuthorizer(&Service{store: store}, "sample", []string{"items:write"}, []string{"items:create"})
			require.NoError(t, err)
			want, reads := ErrConfiguration, 0
			switch variant {
			case "nil authorizer":
				a = nil
			case "nil context":
				ctx = nil
			case "canceled before read":
				cancel()
				want = context.Canceled
			case "canceled during read":
				store.cancel = cancel
				want = context.Canceled
				reads = 1
			}
			require.ErrorIs(t, a.Authorize(ctx, policyFixture().Subject, []string{"items:write"}, []string{"items:create"}), want)
			require.Equal(t, reads, store.reads)
		})
	}
}

// TestMongoDefaultAuthorityRetainsExplicitRestrictions uses native current
// reads and audited capability replacement across one retained authorizer.
func TestMongoDefaultAuthorityRetainsExplicitRestrictions(t *testing.T) {
	for _, variant := range []string{"disabled", "expired", "operator only"} {
		t.Run(variant, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			service, err := NewService(store, func(context.Context, string) (string, error) { return "verified-operator", nil })
			require.NoError(t, err)
			subject := policyFixture().Subject
			scopes, permissions := []string{"member.self"}, []string{"member.enroll"}
			a, err := NewDefaultAuthorizer(service, subject.System, scopes, permissions)
			require.NoError(t, err)
			require.NoError(t, a.Authorize(ctx, subject, scopes, permissions))
			_, err = store.Read(ctx, subject)
			require.ErrorIs(t, err, ErrDenied)
			grant, err := service.ApplyCapabilities(ctx, subject, 0, Capabilities{Enabled: true, Scopes: scopes, Permissions: permissions})
			require.NoError(t, err)
			require.NoError(t, a.Authorize(ctx, subject, scopes, permissions))
			change := Capabilities{Enabled: true, Scopes: scopes, Permissions: permissions}
			switch variant {
			case "disabled":
				change.Enabled = false
			case "expired":
				change.ExpiresAt = time.Now().Add(-time.Minute)
			case "operator only":
				change.Scopes = []string{"program"}
				change.Permissions = []string{"policy.publish"}
			}
			_, err = service.ApplyCapabilities(ctx, subject, grant.Revision, change)
			require.NoError(t, err)
			require.ErrorIs(t, a.Authorize(ctx, subject, scopes, permissions), ErrDenied)
			require.Equal(t, int64(2), collectionCount(t, store, ctx, auditCollection))
		})
	}
}
