package partners

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnerruntime "github.com/ooaklee/ghatd/external/partnermanager/runtime"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

type usersFixture struct{}

func (*usersFixture) GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	panic("unexpected I/O")
}
func (*usersFixture) GetSignupAttribution(context.Context, string) (user.SignupAttribution, error) {
	panic("unexpected I/O")
}

type groupsFixture struct{}

func (*groupsFixture) GetActiveDirectGroupIDs(context.Context, string) ([]string, error) {
	panic("unexpected I/O")
}

type policyFixture struct{}

func (*policyFixture) Authorize(context.Context, accesspolicy.Subject, []string, []string) error {
	panic("unexpected I/O")
}

type sessionsFixture struct{}

func (*sessionsFixture) AuthenticateSession(context.Context, string) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	panic("unexpected I/O")
}

type idFixture struct{}

func (*idFixture) NewID() string { panic("unexpected I/O") }
func fixtureDependencies() Dependencies {
	return Dependencies{Users: &usersFixture{}, Groups: &groupsFixture{}, Policy: &policyFixture{}, Sessions: &sessionsFixture{}, Clock: partnerprogram.ClockFunc(func() time.Time { panic("unexpected I/O") }), IDs: &idFixture{}, AccountAdmission: func(context.Context, string) error { panic("unexpected I/O") }}
}
func TestBindDependenciesValidatesWithoutIO(t *testing.T) {
	cfg := Config{System: "example", ActiveStatus: "active", Identity: partnermanager.UserIdentityConfig{ProgramID: partnerprogram.ProgramID, IndividualAccountTypes: []string{"individual"}, ActiveAccountStatuses: []string{"active"}}}
	for _, tc := range []struct {
		name    string
		change  func(*Dependencies)
		invalid bool
	}{{"valid", nil, false}, {"missing users", func(d *Dependencies) { d.Users = nil }, true}, {"typed nil users", func(d *Dependencies) { var v *usersFixture; d.Users = v }, true}, {"missing sessions", func(d *Dependencies) { d.Sessions = nil }, true}, {"missing admission", func(d *Dependencies) { d.AccountAdmission = nil }, true}, {"typed nil policy", func(d *Dependencies) { var v *policyFixture; d.MemberPolicy = v }, true}, {"missing groups", func(d *Dependencies) { d.Groups = nil }, true}, {"nil clock function", func(d *Dependencies) { d.Clock = partnerprogram.ClockFunc(nil) }, true}, {"missing identifiers", func(d *Dependencies) { d.IDs = nil }, true}} {
		t.Run(tc.name, func(t *testing.T) {
			deps := fixtureDependencies()
			if tc.change != nil {
				tc.change(&deps)
			}
			got, err := BindDependencies(t.Context(), cfg, deps)
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.NotNil(t, got.Sessions)
				require.NotNil(t, got.Authority)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := BindDependencies(ctx, cfg, fixtureDependencies())
	require.ErrorIs(t, err, context.Canceled)
	_, err = BindDependencies(nil, cfg, fixtureDependencies())
	require.ErrorIs(t, err, partnermanager.ErrUnavailable)
}
func TestRuntimeDrainProtectsBorrowedResource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout bool
	}{{"wait for drain", false}, {"timeout leaves drain pending", true}} {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan struct{})
			cancelled := false
			r := &Runtime{cancel: func() { cancelled = true }, done: []<-chan struct{}{done}}
			ctx := t.Context()
			if tc.timeout {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if tc.timeout {
				require.ErrorIs(t, r.Close(ctx), context.Canceled)
				require.True(t, cancelled)
				close(done)
				require.NoError(t, r.Close(t.Context()))
				return
			}
			closed := make(chan error, 1)
			go func() { closed <- r.Close(ctx) }()
			select {
			case <-closed:
				t.Fatal("returned before drain")
			case <-time.After(10 * time.Millisecond):
			}
			close(done)
			require.NoError(t, <-closed)
			require.True(t, cancelled)
			require.Error(t, r.Start(t.Context(), nil))
		})
	}
}
func TestPaidAcquisitionDisabledRequiresNoOwners(t *testing.T) {
	got, err := PaidAcquisition(partnerruntime.Config{}, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Nil(t, got)
	_, err = PaidAcquisition(partnerruntime.Config{RequireAcquisitionEligibility: true}, nil, nil, nil, nil)
	require.ErrorIs(t, err, partnermanager.ErrUnavailable)
}

func TestPaidAcquisitionWithExplicitPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  *partnermanager.PaidAcquisitionConfig
		wantErr bool
	}{
		{name: "disabled"},
		{name: "enabled without owners", policy: &partnermanager.PaidAcquisitionConfig{}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PaidAcquisitionWithConfig(tc.policy, nil, nil, nil)
			if tc.wantErr {
				require.ErrorIs(t, err, partnermanager.ErrUnavailable)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, got)
		})
	}
}

// This sequence proves each borrowed worker is drained before a Mongo release.
func TestBorrowedMongoWaitsForAllWorkers(t *testing.T) {
	for _, tc := range []struct {
		name, active string
		timeout      bool
	}{
		{name: "waits_for_partner_pass_even_when_delivery_idle", active: "partner"},
		{name: "waits_for_delivery_pass_even_when_partners_idle", active: "delivery"},
		{name: "timeout_preserves_borrowed_pool", active: "partner", timeout: true},
		{name: "absent_runtime_can_close_pool", active: "absent"},
		{name: "lifecycle_inflight", active: "lifecycle"},
		{name: "lifecycle_timeout_preserves_pool", active: "lifecycle", timeout: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closed := false
			closePool := func(context.Context) error { closed = true; return nil }
			if tc.active == "absent" {
				var r *Runtime
				require.NoError(t, r.Close(t.Context()))
				require.NoError(t, closePool(t.Context()))
				require.True(t, closed)
				return
			}
			partnerDone, deliveryDone, lifecycleDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			pending := partnerDone
			switch tc.active {
			case "partner":
				close(deliveryDone)
				close(lifecycleDone)
			case "delivery":
				close(partnerDone)
				close(lifecycleDone)
				pending = deliveryDone
			case "lifecycle":
				close(partnerDone)
				close(deliveryDone)
				pending = lifecycleDone
			}
			cancelled := make(chan struct{})
			r := &Runtime{cancel: func() { close(cancelled) }, done: []<-chan struct{}{partnerDone, deliveryDone, lifecycleDone}}
			drainThenClose := func(ctx context.Context) error {
				if err := r.Close(ctx); err != nil {
					return err
				}
				return closePool(ctx)
			}
			if tc.timeout {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				require.ErrorIs(t, drainThenClose(ctx), context.Canceled)
				require.False(t, closed)
				close(pending)
				return
			}
			result := make(chan error, 1)
			go func() { result <- drainThenClose(t.Context()) }()
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("workers not cancelled")
			}
			require.False(t, closed)
			select {
			case <-result:
				t.Fatal("drain returned before completion")
			default:
			}
			close(pending)
			select {
			case err := <-result:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("drain did not finish")
			}
			require.True(t, closed)
		})
	}
}
