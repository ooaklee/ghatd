package billing

import (
	"context"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// CheckoutBindingInspection contains only diagnostic states, never identifiers
// or frozen requests. "missing" means a completed owning read returned sole
// absence. "not_located" means there was no identifier to perform that read.
// A successful inspection grants no financial or access authority.
type CheckoutBindingInspection struct {
	ReverseRecord           string `json:"reverse_record"`
	ReverseIntent           string `json:"reverse_intent"`
	ReverseAcknowledgement  string `json:"reverse_acknowledgement"`
	ReverseTerms            string `json:"reverse_terms"`
	MetadataIntent          string `json:"metadata_intent"`
	MetadataAcknowledgement string `json:"metadata_acknowledgement"`
	MetadataTerms           string `json:"metadata_terms"`
	ReferencesAgree         string `json:"references_agree"`
}

// InspectCheckoutBinding reads existing checkout records in one snapshot and
// compares them with diagnostic provider evidence. It never acknowledges,
// associates, inserts or repairs records. Hosts must authorize this private
// diagnostic before supplying provider evidence or displaying its states.
func (s *CheckoutService) InspectCheckoutBinding(ctx context.Context, e paymentprovider.RevenueCheckoutEvidence) (CheckoutBindingInspection, error) {
	if err := s.ready(ctx); err != nil {
		return CheckoutBindingInspection{}, err
	}
	scope := RevenueScope{Provider: e.Scope.Provider, AccountID: e.Scope.AccountID, LiveMode: e.Scope.LiveMode}
	if !validRevenueScope(scope) || !validCheckoutText(e.SessionID, 256, true) {
		return CheckoutBindingInspection{}, ErrRevenueInvalid
	}
	var out CheckoutBindingInspection
	err := s.repo.ReadCheckout(ctx, func(tx CheckoutTx) error {
		out = CheckoutBindingInspection{ReverseRecord: "missing", ReverseIntent: "not_located", ReverseAcknowledgement: "not_located", ReverseTerms: "not_checked", MetadataIntent: "not_supplied", MetadataAcknowledgement: "not_located", MetadataTerms: "not_checked", ReferencesAgree: "not_comparable"}
		reverse, ok := tx.(CheckoutSessionTx)
		if !ok || revenueNil(reverse) {
			return ErrRevenueUnavailable
		}
		reverseID, err := reverse.FindCheckoutIntentIDBySession(ctx, scope, e.SessionID)
		if err != nil && !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		}
		if err == nil {
			if !validCheckoutText(reverseID, 256, true) {
				return ErrRevenueUnavailable
			}
			out.ReverseRecord = "present"
			out.ReverseIntent, out.ReverseAcknowledgement, out.ReverseTerms, err = inspectCheckoutIntent(ctx, tx, reverseID, scope, e, true)
			if err != nil {
				return err
			}
		}
		if e.IntentID != "" {
			if !validCheckoutText(e.IntentID, 256, true) {
				out.MetadataIntent = "invalid_reference"
			} else {
				out.MetadataIntent, out.MetadataAcknowledgement, out.MetadataTerms, err = inspectCheckoutIntent(ctx, tx, e.IntentID, scope, e, false)
				if err != nil {
					return err
				}
			}
			if out.ReverseRecord == "present" {
				out.ReferencesAgree = "conflict"
				if reverseID == e.IntentID {
					out.ReferencesAgree = "match"
				}
			}
		}
		return ctx.Err()
	})
	if err != nil {
		return CheckoutBindingInspection{}, err
	}
	if err := ctx.Err(); err != nil {
		return CheckoutBindingInspection{}, err
	}
	return out, nil
}

func inspectCheckoutIntent(ctx context.Context, tx CheckoutTx, id string, scope RevenueScope, e paymentprovider.RevenueCheckoutEvidence, fromReverse bool) (intentState, ackState, termsState string, err error) {
	v, err := tx.GetCheckoutIntent(ctx, id)
	if singleRevenueCause(err, ErrRevenueNotFound) {
		return "missing", "not_located", "not_checked", nil
	}
	if err != nil {
		return "", "", "", err
	}
	if !validStoredCheckout(v) {
		return "", "", "", ErrRevenueUnavailable
	}
	session, err := tx.GetCheckoutAcknowledgement(ctx, id)
	ackState = "missing"
	if err == nil {
		ackState = "match"
		if session != e.SessionID || v.Scope != scope {
			ackState = "conflict"
		}
	} else if !singleRevenueCause(err, ErrRevenueNotFound) {
		return "", "", "", err
	}
	if fromReverse && ackState == "match" {
		join, ok := tx.(CheckoutAcknowledgementTx)
		if !ok || revenueNil(join) {
			return "", "", "", ErrRevenueUnavailable
		}
		v.SessionID = session
		if err := join.ValidateCheckoutAcknowledgement(ctx, v); err != nil {
			return "", "", "", err
		}
	}
	q := v.Request
	termsState = "mismatch"
	if v.Scope == scope && q.UserReference == e.ClientReferenceID && q.PriceID == e.PriceID && strings.ToUpper(q.ExpectedCurrency) == e.Currency && q.Mode == e.Mode && q.ExpectedAmount == e.UnitAmountMinor && string(q.ExpectedBillingCadence) == e.BillingCadence && e.IntervalCount == 1 && !e.CreatedAt.IsZero() && !e.CreatedAt.Before(v.CreatedAt.Truncate(time.Second)) {
		termsState = "match"
	}
	return "present", ackState, termsState, nil
}
