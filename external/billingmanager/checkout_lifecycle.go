package billingmanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutLifecycleTarget comes from the canonical retained intent. It is
// private scope, never a transport assertion or an alias for the verified actor.
type CheckoutLifecycleTarget struct {
	Scope                 billing.RevenueScope `json:"-"`
	PrincipalID, IntentID string               `json:"-"`
}
type CheckoutLifecycleAuthority interface {
	AuthorizeCheckoutLifecycle(context.Context, string, string, CheckoutLifecycleTarget) error
}

// CheckoutLifecycleService is derived ONLY from the same revenue association
// owner configured by WithRevenueServices, independent of paid capture wiring.
type CheckoutLifecycleService interface {
	PrepareCheckoutLifecycle(context.Context, billing.CheckoutIntent) (billing.CheckoutIntent, error)
	LookupCheckoutLifecycleEvidence(context.Context, billing.CheckoutIntent) (paymentprovider.RevenueCheckoutEvidence, error)
	CaptureCheckoutLifecycleEvidence(context.Context, billing.CheckoutIntent, paymentprovider.RevenueCheckoutEvidence) (billing.CheckoutLifecycleAnchor, error)
	FindCheckoutLifecycleReceipt(context.Context, billing.CheckoutIntent) (billing.CheckoutLifecycleAnchor, error)
}

// WithCheckoutLifecycleAuthority configures private completion stages before
// serving work. It creates no grant, host outbox, worker or HTTP route.
func (s *Service) WithCheckoutLifecycleAuthority(a CheckoutLifecycleAuthority) (*Service, error) {
	if s == nil || nilRevenueDependency(a) || nilRevenueDependency(s.revenueAssociation) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := s.revenueAssociation.(CheckoutLifecycleService)
	if !ok || nilRevenueDependency(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	s.checkoutLifecycleAuthority = a
	return s, nil
}

func checkoutLifecycleTarget(i billing.CheckoutIntent) CheckoutLifecycleTarget {
	return CheckoutLifecycleTarget{Scope: i.Scope, PrincipalID: i.Request.UserID, IntentID: i.ID}
}
func (s *Service) checkoutLifecycleAuthorize(ctx context.Context, actor string, i billing.CheckoutIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkoutLifecycleAuthority.AuthorizeCheckoutLifecycle(ctx, actor, SubscriptionStatusRefresh, checkoutLifecycleTarget(i)); err != nil {
		return err
	}
	return ctx.Err()
}
func (s *Service) checkoutLifecycleBegin(ctx context.Context, actor string, i billing.CheckoutIntent) (CheckoutLifecycleService, error) {
	if ctx == nil || actor == "" || len(actor) > 256 || strings.TrimSpace(actor) != actor || strings.ContainsAny(actor, "\r\n\x00") {
		return nil, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || nilRevenueDependency(s.checkoutLifecycleAuthority) || nilRevenueDependency(s.revenueAssociation) {
		return nil, billing.ErrRevenueUnavailable
	}
	owner, ok := s.revenueAssociation.(CheckoutLifecycleService)
	if !ok || nilRevenueDependency(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	if err := i.ValidateAcknowledgedSubscription(); err != nil {
		return nil, err
	}
	if err := s.checkoutLifecycleAuthorize(ctx, actor, i); err != nil {
		return nil, err
	}
	return owner, nil
}
func (s *Service) checkoutLifecycleFinish(ctx context.Context, actor string, i billing.CheckoutIntent, operationErr error) error {
	if err := s.checkoutLifecycleAuthorize(ctx, actor, i); err != nil {
		return err
	}
	return operationErr
}

// PrepareCheckoutLifecycle returns the detached native original after current
// selected refresh authority and owning joins. Retain it before provider I/O.
func (s *Service) PrepareCheckoutLifecycle(ctx context.Context, actor string, i billing.CheckoutIntent) (billing.CheckoutIntent, error) {
	owner, err := s.checkoutLifecycleBegin(ctx, actor, i)
	if err != nil {
		return billing.CheckoutIntent{}, err
	}
	out, err := owner.PrepareCheckoutLifecycle(ctx, i)
	if err == nil {
		err = out.ValidateAcknowledgedInput(i)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return billing.CheckoutIntent{}, err
	}
	return out, nil
}

// ValidateCheckoutLifecycle rechecks the same original for a retained job or
// replacement worker; it does not impersonate the original preparing caller.
func (s *Service) ValidateCheckoutLifecycle(ctx context.Context, actor string, i billing.CheckoutIntent) error {
	_, err := s.PrepareCheckoutLifecycle(ctx, actor, i)
	return err
}

// LookupCheckoutLifecycleEvidence requires current authority before original
// validation, again before provider I/O, and after outcome. Retain the returned
// evidence durably before capture. Recovery must inspect/replay originals first.
func (s *Service) LookupCheckoutLifecycleEvidence(ctx context.Context, actor string, i billing.CheckoutIntent) (paymentprovider.RevenueCheckoutEvidence, error) {
	owner, err := s.checkoutLifecycleBegin(ctx, actor, i)
	if err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	original, err := owner.PrepareCheckoutLifecycle(ctx, i)
	if err == nil {
		err = original.ValidateAcknowledgedInput(i)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	out, err := owner.LookupCheckoutLifecycleEvidence(ctx, i)
	if err == nil {
		err = i.ValidateLifecycleEvidence(out)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return paymentprovider.RevenueCheckoutEvidence{}, err
	}
	return out, nil
}

// CaptureCheckoutLifecycleEvidence submits only retained original inputs. Exact
// replay recovers the original receipt without a provider request; an uncertain
// outcome or post-commit denial withholds output and requires original recovery.
func (s *Service) CaptureCheckoutLifecycleEvidence(ctx context.Context, actor string, i billing.CheckoutIntent, e paymentprovider.RevenueCheckoutEvidence) (billing.CheckoutLifecycleAnchor, error) {
	owner, err := s.checkoutLifecycleBegin(ctx, actor, i)
	if err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	if err = i.ValidateLifecycleEvidence(e); err != nil {
		return billing.CheckoutLifecycleAnchor{}, s.checkoutLifecycleFinish(ctx, actor, i, err)
	}
	original, err := owner.PrepareCheckoutLifecycle(ctx, i)
	if err == nil {
		err = original.ValidateAcknowledgedInput(i)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	out, err := owner.CaptureCheckoutLifecycleEvidence(ctx, i, e)
	if err == nil {
		err = out.ValidateCapturedEvidence(i, e)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	return out, nil
}

// FindCheckoutLifecycleReceipt checks current refresh permission even on replay
// or conclusive absence. It performs no provider call and never replaces saved
// uncertain evidence with a newly fetched response.
func (s *Service) FindCheckoutLifecycleReceipt(ctx context.Context, actor string, i billing.CheckoutIntent) (billing.CheckoutLifecycleAnchor, error) {
	owner, err := s.checkoutLifecycleBegin(ctx, actor, i)
	if err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	out, err := owner.FindCheckoutLifecycleReceipt(ctx, i)
	if err == nil {
		err = out.ValidateForCheckout(i)
	}
	if err = s.checkoutLifecycleFinish(ctx, actor, i, err); err != nil {
		return billing.CheckoutLifecycleAnchor{}, err
	}
	return out, nil
}

var _ CheckoutLifecycleService = (*billing.CheckoutService)(nil)
