package partnermanagerhelper

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
)

type admittedOwner struct {
	principal       partnermanager.Principal
	fact            partnermanager.SignupFact
	err             error
	reads, captures int
	cancel          context.CancelFunc
}

func (o *admittedOwner) GetPartnerPrincipal(context.Context, string) (partnermanager.Principal, error) {
	o.reads++
	if o.cancel != nil {
		o.cancel()
	}
	return o.principal, o.err
}
func (o *admittedOwner) GetSignupFact(context.Context, string) (partnermanager.SignupFact, error) {
	o.captures++
	if o.cancel != nil {
		o.cancel()
	}
	return o.fact, o.err
}

func TestAccountIdentityAdmissionAndImmutableCapture(t *testing.T) {
	outage := errors.New("admission unavailable")
	for _, tc := range []struct {
		name, state string
		want        error
		capture     bool
	}{
		{name: "admitted_selected_principal"},
		{name: "selected_principal_denied", state: "denied", want: partnermanager.ErrDenied},
		{name: "admission_outage_preserved", state: "outage", want: outage},
		{name: "selected_owner_error", state: "owner", want: outage},
		{name: "owner_id_mismatch", state: "mismatch", want: partnermanager.ErrUnavailable},
		{name: "cancelled_owner_discards_principal", state: "owner-cancel", want: context.Canceled},
		{name: "cancelled_admission_discards_principal", state: "admission-cancel", want: context.Canceled},
		{name: "capture_ignores_current_denial", state: "denied", capture: true},
		{name: "capture_ignores_unavailable_admission", state: "outage", capture: true},
		{name: "capture_requires_owner_evidence", state: "owner", capture: true, want: outage},
		{name: "capture_late_cancellation", state: "owner-cancel", capture: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			owner := &admittedOwner{principal: partnermanager.Principal{ID: "customer"}, fact: partnermanager.SignupFact{ID: "original", CustomerID: "customer", AttributionEvidence: "retained"}}
			admissionReads := 0
			admission := partneraccess.AccountAdmission(func(_ context.Context, id string) error {
				admissionReads++
				require.Equal(t, "customer", id)
				switch tc.state {
				case "denied":
					return partnermanager.ErrDenied
				case "outage":
					return outage
				case "admission-cancel":
					cancel()
				}
				return nil
			})
			if tc.state == "owner" {
				owner.err = outage
			}
			if tc.state == "mismatch" {
				owner.principal.ID = "other"
			}
			if tc.state == "owner-cancel" {
				owner.cancel = cancel
			}
			identity, err := NewAccountIdentity(owner, admission)
			require.NoError(t, err)
			require.Zero(t, owner.reads)
			require.Zero(t, admissionReads)
			if tc.capture {
				fact, e := identity.GetSignupFact(ctx, "customer")
				err = e
				if e == nil {
					require.Equal(t, owner.fact, fact)
				} else {
					require.Zero(t, fact)
				}
				require.Zero(t, owner.reads)
				require.Equal(t, 1, owner.captures)
				require.Zero(t, admissionReads)
			} else {
				p, e := identity.GetPartnerPrincipal(ctx, "customer")
				err = e
				if e == nil {
					require.Equal(t, owner.principal, p)
				} else {
					require.Zero(t, p)
				}
				require.Equal(t, 1, owner.reads)
				require.Zero(t, owner.captures)
				if tc.state == "owner" || tc.state == "mismatch" || tc.state == "owner-cancel" {
					require.Zero(t, admissionReads)
				} else {
					require.Equal(t, 1, admissionReads)
				}
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
func TestAccountIdentityRejectsInvalidWiringAndIDsWithoutIO(t *testing.T) {
	for _, tc := range []struct {
		name, state, id string
		want            error
	}{
		{"nil_context", "nil-context", "customer", partnermanager.ErrInvalid},
		{"cancelled_context", "cancelled", "customer", context.Canceled},
		{"empty_id", "", "", partnermanager.ErrInvalid},
		{"internal_whitespace", "", "customer one", partnermanager.ErrInvalid},
		{"unicode_whitespace", "", "customer\u00a01", partnermanager.ErrInvalid},
		{"control_id", "", "customer\x001", partnermanager.ErrInvalid},
		{"invalid_utf8", "", "customer\xff1", partnermanager.ErrInvalid},
		{"oversized_id", "", strings.Repeat("a", 257), partnermanager.ErrInvalid},
		{"nil_identity", "nil-identity", "customer", partnermanager.ErrUnavailable},
		{"missing_owner", "nil-owner", "customer", partnermanager.ErrUnavailable},
		{"typed_nil_owner", "typed-owner", "customer", partnermanager.ErrUnavailable},
		{"missing_admission", "nil-admission", "customer", partnermanager.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &admittedOwner{}
			reads := 0
			identity, err := NewAccountIdentity(owner, func(context.Context, string) error { reads++; return nil })
			require.NoError(t, err)
			ctx := t.Context()
			switch tc.state {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-identity":
				identity = nil
			case "nil-owner":
				identity.owner = nil
			case "typed-owner":
				identity.owner = (*admittedOwner)(nil)
			case "nil-admission":
				identity.admission = nil
			}
			p, err := identity.GetPartnerPrincipal(ctx, tc.id)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, p)
			if tc.state != "nil-admission" {
				fact, e := identity.GetSignupFact(ctx, tc.id)
				require.ErrorIs(t, e, tc.want)
				require.Zero(t, fact)
			}
			require.Zero(t, owner.reads)
			require.Zero(t, owner.captures)
			require.Zero(t, reads)
			if tc.state == "nil-owner" || tc.state == "typed-owner" || tc.state == "nil-admission" {
				_, err = NewAccountIdentity(identity.owner, identity.admission)
				require.ErrorIs(t, err, partnermanager.ErrUnavailable)
			}
		})
	}
}
