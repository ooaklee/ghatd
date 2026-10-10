package partnerhttp

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// All Partners entry points bind the already resolved credential to the live
// shared authority. The owning manager checks the exact action and target;
// neither a host domain transaction nor a generic administrator role grants it.
func (m *Service) partnersSession(ctx context.Context, p Principal) (context.Context, *partnermanager.Manager, error) {
	if err := member(p); err != nil {
		return nil, nil, err
	}
	if m.manager == nil {
		return nil, nil, fail("PARTNERS_DEPENDENCY_UNAVAILABLE", 503)
	}
	bound, err := partneraccess.WithVerifiedSession(ctx, p.ActorID, p.Credential)
	if err != nil {
		return nil, nil, partnerError(err)
	}
	return bound, m.manager, nil
}

// partnerError translates owning-service errors into transport Error values.
// Uncertain outcomes map to 503, dependency failures and cancellation to 503,
// unknown wrapped causes to 500 before denials, denials to 403, not-found to
// 404, invalid to 400, stale writes to 412, and conflicts/insufficient funds to
// 409. Only transport errors on the allowlist pass through; all other host
// diagnostics collapse to 500.
func partnerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, partnerprogram.ErrUncertain), errors.Is(err, partnerearnings.ErrUncertain), errors.Is(err, referral.ErrUncertain):
		return fail("PARTNERS_OUTCOME_UNCERTAIN", 503)
	case errors.Is(err, partnermanager.ErrUnavailable), errors.Is(err, partnerprogram.ErrUnavailable), errors.Is(err, partnerearnings.ErrUnavailable), errors.Is(err, partnerearnings.ErrUnresolved), errors.Is(err, referral.ErrUnavailable), errors.Is(err, partnerearnings.ErrReportTooLarge), errors.Is(err, referral.ErrCapacity), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return fail("PARTNERS_DEPENDENCY_UNAVAILABLE", 503)
	case partnerUnknownCause(err, 0):
		return fail("PARTNERS_INTERNAL_ERROR", 500)
	case errors.Is(err, partnermanager.ErrIneligible):
		return fail("PARTNERS_ACQUISITION_INELIGIBLE", 403)
	case errors.Is(err, partnermanager.ErrDenied), errors.Is(err, partnerprogram.ErrDenied), errors.Is(err, partnerearnings.ErrDenied), errors.Is(err, referral.ErrDenied):
		return fail("PARTNERS_FORBIDDEN", 403)
	case errors.Is(err, partnermanager.ErrNotPartner), errors.Is(err, partnermanager.ErrNotFound), errors.Is(err, partnerprogram.ErrNotFound), errors.Is(err, partnerearnings.ErrNotFound), errors.Is(err, referral.ErrNotFound):
		return fail("PARTNERS_NOT_FOUND", 404)
	case errors.Is(err, partnermanager.ErrInvalid), errors.Is(err, partnerprogram.ErrInvalid), errors.Is(err, partnerearnings.ErrInvalid), errors.Is(err, referral.ErrInvalid), errors.Is(err, referral.ErrSelfReferral):
		return fail("PARTNERS_INVALID_REQUEST", 400)
	case errors.Is(err, partnerearnings.ErrStaleWrite), errors.Is(err, partnerprogram.ErrStaleWrite), errors.Is(err, referral.ErrStaleWrite):
		return fail("PARTNERS_STALE_WRITE", 412)
	case errors.Is(err, partnerearnings.ErrInsufficient):
		return fail("PARTNERS_INSUFFICIENT_FUNDS", 409)
	case errors.Is(err, partnerprogram.ErrAlreadyEnrolled), errors.Is(err, partnerprogram.ErrAlreadyExists), errors.Is(err, partnerearnings.ErrAlreadyExists), errors.Is(err, referral.ErrAlreadyExists), errors.Is(err, partnerearnings.ErrConflict), errors.Is(err, partnerearnings.ErrCurrencyMismatch), errors.Is(err, partnerearnings.ErrInvalidState), errors.Is(err, referral.ErrAlreadyReferred), errors.Is(err, referral.ErrCodeRetired), errors.Is(err, partnerprogram.ErrAmbiguousPolicy):
		return fail("PARTNERS_CONFLICT", 409)
	}
	// Preserve only transport errors, never return a private driver's diagnostic.
	var host *Error
	if errors.As(err, &host) && safePartnerHostError(host) {
		return fail(host.Code, host.Status)
	}
	return fail("PARTNERS_INTERNAL_ERROR", 500)
}

// partnerLimit parses the singular limit query parameter, defaulting to 50. It
// returns -1 for duplicates, non-numeric values, or values outside 1..100,
// which callers treat as invalid.
func partnerLimit(req Request) int {
	values := req.Query["limit"]
	if len(values) > 1 {
		return -1
	}
	if len(values) == 0 {
		return 50
	}
	n, err := strconv.Atoi(values[0])
	if err != nil || n < 1 || n > 100 {
		return -1
	}
	return n
}

// partnerQueryText returns the single value for a query key, or empty when
// absent. More than one value or a value longer than max bytes fails with
// PARTNERS_INVALID_REQUEST.
func partnerQueryText(req Request, key string, max int) (string, error) {
	values := req.Query[key]
	if len(values) > 1 {
		return "", fail("PARTNERS_INVALID_REQUEST", 400)
	}
	if len(values) == 0 {
		return "", nil
	}
	if len(values[0]) > max {
		return "", fail("PARTNERS_INVALID_REQUEST", 400)
	}
	return values[0], nil
}

// Claim filters are a bounded union of known states. Reject typos and duplicate
// values instead of reporting a misleading empty queue from an unknown state.
func partnerClaimStates(req Request) ([]string, error) {
	values := req.Query["state"]
	if len(values) > 6 {
		return nil, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	seen := make(map[string]bool, len(values))
	for _, state := range values {
		switch state {
		case partnerearnings.ClaimRequested, partnerearnings.ClaimProcessing,
			partnerearnings.ClaimNeedsReview, partnerearnings.ClaimPaid,
			partnerearnings.ClaimRejected, partnerearnings.ClaimCancelled:
		default:
			return nil, fail("PARTNERS_INVALID_REQUEST", 400)
		}
		if seen[state] {
			return nil, fail("PARTNERS_INVALID_REQUEST", 400)
		}
		seen[state] = true
	}
	return append([]string(nil), values...), nil
}

// partnerDate parses an RFC3339Nano timestamp, rejecting parse failures and the
// zero time, and normalizes the result to UTC.
func partnerDate(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || t.IsZero() {
		return time.Time{}, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	return t.UTC(), nil
}

// partnerDates parses optional singular from/to query timestamps as UTC values.
// Providing both requires to to be strictly after from; duplicates, overlong
// values, malformed timestamps or an inverted range fail with
// PARTNERS_INVALID_REQUEST.
func partnerDates(req Request) (*time.Time, *time.Time, error) {
	var from, to *time.Time
	for _, key := range []string{"from", "to"} {
		raw, err := partnerQueryText(req, key, 64)
		if err != nil {
			return nil, nil, err
		}
		if raw != "" {
			t, err := partnerDate(raw)
			if err != nil {
				return nil, nil, err
			}
			if key == "from" {
				from = &t
			} else {
				to = &t
			}
		}
	}
	if from != nil && to != nil && !to.After(*from) {
		return nil, nil, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	return from, to, nil
}

// partnerStatementQuery builds a StatementQuery from limit, kind, from/to dates
// and an optional positive before_sequence cursor. Any invalid component fails
// with PARTNERS_INVALID_REQUEST.
func partnerStatementQuery(req Request) (partnerearnings.StatementQuery, error) {
	q := partnerearnings.StatementQuery{Limit: partnerLimit(req), Kinds: req.Query["kind"]}
	if q.Limit < 1 {
		return q, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	var err error
	q.From, q.To, err = partnerDates(req)
	if err != nil {
		return q, err
	}
	raw, err := partnerQueryText(req, "before_sequence", 20)
	if err != nil {
		return q, err
	}
	if raw != "" {
		q.BeforeSequence, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || q.BeforeSequence < 1 {
			return q, fail("PARTNERS_INVALID_REQUEST", 400)
		}
	}
	return q, nil
}

// partnerReferralQuery builds a ReferralSummaryQuery from a validated limit, an
// optional bounded after cursor and optional from/to dates, forwarding any
// parse failure.
func partnerReferralQuery(req Request) (partnermanager.ReferralSummaryQuery, error) {
	q := partnermanager.ReferralSummaryQuery{Limit: partnerLimit(req)}
	if q.Limit < 1 {
		return q, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	var err error
	q.After, err = partnerQueryText(req, "after", 256)
	if err != nil {
		return q, err
	}
	q.From, q.To, err = partnerDates(req)
	return q, err
}

// Read serves member-facing partners read operations. It resolves the live
// session authority, dispatches to partnersReadBody, and converts every error
// through partnerError so only mapped transport codes escape.
func (m *Service) Read(ctx context.Context, p Principal, req Request) (Response, error) {
	ctx, svc, err := m.partnersSession(ctx, p)
	if err != nil {
		return Response{}, err
	}
	response, err := m.partnersReadBody(ctx, svc, p, req)
	return response, partnerError(err)
}

// partnersReadBody dispatches a fixed member read operation (program, overview,
// share link, referrals, ledger, claims, claim, destination) after validating
// query inputs. Program disclosure additionally rechecks the host partner
// session; all other reads delegate to the owning Manager scoped to the
// verified actor. Unknown operations are rejected as invalid requests.
func (m *Service) partnersReadBody(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	switch req.Operation {
	case "partners.eligibility.read":
		eligible, err := svc.AcquisitionEligible(ctx, p.ActorID)
		if err != nil {
			return Response{}, err
		}
		return reply(200, map[string]bool{"eligible": eligible}, "")
	case "partners.program.read":
		if m.sessions == nil {
			return Response{}, partnermanager.ErrUnavailable
		}
		if err := m.sessions.CheckPartnerSession(ctx, p.ActorID, p.Credential); err != nil {
			return Response{}, err
		}
		return reply(200, m.programDisclosure(), "")
	case "partners.overview.read":
		ov, err := svc.Overview(ctx, p.ActorID)
		if err != nil {
			return Response{}, err
		}
		var destination any
		if ov.Destination != nil {
			destination = destinationView(*ov.Destination)
		}
		return reply(200, map[string]any{"partner": partnerView(ov.Partner), "terms": termsView(ov.Terms), "balances": ov.Balances, "destination": destination, "as_of": ov.AsOf, "program": m.programDisclosure()}, "")
	case "partners.share-link.read":
		l, err := svc.GetOrCreateLink(ctx, p.ActorID)
		if err != nil {
			return Response{}, err
		}
		return reply(200, m.linkView(l), "")
	case "partners.referrals.read":
		q, err := partnerReferralQuery(req)
		if err != nil {
			return Response{}, err
		}
		page, err := svc.ReferralSummaries(ctx, p.ActorID, q)
		if err != nil {
			return Response{}, err
		}
		return reply(200, page, "")
	case "partners.ledger.read":
		q, err := partnerStatementQuery(req)
		if err != nil {
			return Response{}, err
		}
		s, err := svc.Statement(ctx, p.ActorID, q)
		if err != nil {
			return Response{}, err
		}
		return reply(200, statementView(s), "")
	case "partners.claims.read":
		limit := partnerLimit(req)
		if limit < 1 {
			return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
		}
		after, err := partnerQueryText(req, "after", 256)
		if err != nil {
			return Response{}, err
		}
		states, err := partnerClaimStates(req)
		if err != nil {
			return Response{}, err
		}
		rows, err := svc.ListClaims(ctx, p.ActorID, states, limit, after)
		if err != nil {
			return Response{}, err
		}
		return reply(200, claimsView(rows, limit, false), "")
	case "partners.claim.read":
		c, err := svc.GetClaim(ctx, p.ActorID, req.ID)
		if err != nil {
			return Response{}, err
		}
		return reply(200, claimView(c, false), "")
	case "partners.destination.read":
		d, err := svc.Destination(ctx, p.ActorID)
		if err != nil {
			return Response{}, err
		}
		return reply(200, destinationView(d), "")
	}
	return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
}

// Command serves member-facing partners commands. It resolves the live session
// authority, dispatches to partnersCommandBody, and converts every error
// through partnerError so only mapped transport codes escape.
func (m *Service) Command(ctx context.Context, p Principal, req Request) (Response, error) {
	ctx, svc, err := m.partnersSession(ctx, p)
	if err != nil {
		return Response{}, err
	}
	response, err := m.partnersCommandBody(ctx, svc, p, req)
	return response, partnerError(err)
}

// partnersCommandBody decodes and forwards a fixed member command: enroll,
// share-link rotate, claim create/cancel or destination update. Bodies with
// unknown fields, missing required values (terms version, expected destination
// version) or invalid expected versions fail as invalid requests; commands run
// against the owning Manager with the request idempotency key. Unknown
// operations are rejected.
func (m *Service) partnersCommandBody(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	switch req.Operation {
	case "partners.enroll":
		var input struct {
			AcceptedTermsVersion string `json:"accepted_terms_version"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		if input.AcceptedTermsVersion == "" {
			return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
		}
		partner, err := svc.EnrollSelf(ctx, p.ActorID, input.AcceptedTermsVersion)
		if err != nil {
			return Response{}, err
		}
		return reply(201, partnerView(partner), "")
	case "partners.share-link.rotate":
		var input struct {
			Reason           string `json:"reason"`
			ExpectedLinkCode string `json:"expected_link_code"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		l, err := svc.RotateLink(ctx, p.ActorID, referral.RotateLinkRequest{ExpectedLinkCode: input.ExpectedLinkCode, Reason: input.Reason, IdempotencyKey: req.Key})
		if err != nil {
			return Response{}, err
		}
		return reply(200, m.linkView(l), "")
	case "partners.claims.create":
		var input struct {
			AmountMinor                int64 `json:"amount_minor"`
			ExpectedDestinationVersion int64 `json:"expected_destination_version"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		c, err := svc.RequestClaimWithDestination(ctx, p.ActorID, partnermanager.SelfClaimRequest{AmountMinor: input.AmountMinor, ExpectedDestinationVersion: input.ExpectedDestinationVersion, IdempotencyKey: req.Key})
		if err != nil {
			return Response{}, err
		}
		return reply(201, claimView(c, false), "")
	case "partners.claims.cancel":
		var input struct {
			Reason           string `json:"reason"`
			ExpectedRevision int64  `json:"expected_revision"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		c, err := svc.CancelClaimWithReceipt(ctx, p.ActorID, partnermanager.SelfCancellationRequest{ClaimID: req.ID, ExpectedRevision: input.ExpectedRevision, Reason: input.Reason, IdempotencyKey: req.Key})
		if err != nil {
			return Response{}, err
		}
		return reply(200, claimView(c, false), "")
	case "partners.destination.update":
		var input struct {
			PayPalEmail     string `json:"paypal_email"`
			ExpectedVersion *int64 `json:"expected_version"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		if input.ExpectedVersion == nil || *input.ExpectedVersion < 0 {
			return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
		}
		d, err := svc.UpdateDestination(ctx, p.ActorID, input.PayPalEmail, *input.ExpectedVersion)
		if err != nil {
			return Response{}, err
		}
		return reply(200, destinationView(d), "")
	}
	return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
}

// An operator needs an explicit, current capability for each selected target.
// The administrator flag is neither necessary nor sufficient for Partners.
func (m *Service) Admin(ctx context.Context, p Principal, req Request) (Response, error) {
	ctx, svc, err := m.partnersSession(ctx, p)
	if err != nil {
		return Response{}, err
	}
	response, err := m.partnersAdminBody(ctx, svc, p, req)
	return response, partnerError(err)
}

// partnersAdminBody dispatches fixed operator operations, binding the verified
// actor into every command and revalidating decoded inputs before delegating to
// Manager. Mutating commands (claim create/decide/amend/return, status change)
// carry the actor and idempotency key; attribution apply verifies the returned
// correction echoes the exact request and reports uncertainty otherwise;
// receipts are confirmed through the accepted* projections. Unknown operations
// are rejected as invalid requests.
func (m *Service) partnersAdminBody(ctx context.Context, svc *partnermanager.Manager, p Principal, req Request) (Response, error) {
	switch req.Operation {
	case "admin.partners.access.read":
		return m.partnersOperatorAccess(ctx, p, req)
	case "admin.partners.claim.read", "admin.partners.status.read", "admin.partners.claim-preparation.read", "admin.partners.policy.individual.read":
		return partnersSelectedRead(ctx, svc, p, req)
	case "admin.partners.policy.read":
		versions, err := svc.AdminPolicyVersions(ctx, p.ActorID)
		if err != nil {
			return Response{}, err
		}
		items := make([]map[string]any, 0, len(versions))
		for _, v := range versions {
			items = append(items, policyView(v))
		}
		return reply(200, map[string]any{"versions": items}, "")
	case "admin.partners.inspect":
		q, err := partnerStatementQuery(req)
		if err != nil {
			return Response{}, err
		}
		s, err := svc.AdminStatement(ctx, p.ActorID, req.ID, q)
		if err != nil {
			return Response{}, err
		}
		rq, err := partnerReferralQuery(req)
		if err != nil {
			return Response{}, err
		}
		r, err := svc.AdminReferralSummaries(ctx, p.ActorID, req.ID, rq)
		if err != nil {
			return Response{}, err
		}
		return reply(200, map[string]any{"statement": statementView(s), "referrals": r}, "")
	case "admin.partners.claims.queue":
		limit := partnerLimit(req)
		if limit < 1 {
			return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
		}
		after, err := partnerQueryText(req, "after", 256)
		if err != nil {
			return Response{}, err
		}
		states, err := partnerClaimStates(req)
		if err != nil {
			return Response{}, err
		}
		rows, err := svc.AdminQueue(ctx, p.ActorID, states, limit, after)
		if err != nil {
			return Response{}, err
		}
		page := claimsView(rows, limit, false)
		items := make([]map[string]any, 0, len(rows))
		for _, c := range rows {
			items = append(items, map[string]any{"claim": claimView(c, false), "partner_id": c.PartnerID, "assigned_to_you": c.ProcessingActor == p.ActorID, "requested_reason": c.RequestedReason, "reason": c.Reason, "review_reason": c.ReviewReason})
		}
		page["claims"] = items
		return reply(200, page, "")
	case "admin.partners.operations.read":
		backlog, err := svc.AdminWorkerBacklog(ctx, p.ActorID)
		if err != nil {
			return Response{}, err
		}
		return reply(200, backlog, "")
	case "admin.partners.policy.publish":
		return partnersPublishPolicy(ctx, svc, p, req)
	case "admin.partners.attribution.preview", "admin.partners.attribution.apply":
		return partnersAttribution(ctx, svc, p, req)
	case "admin.partners.claims.create":
		var input struct {
			PartnerID                  string `json:"partner_id"`
			AmountMinor                int64  `json:"amount_minor"`
			ExpectedDestinationVersion int64  `json:"expected_destination_version"`
			Reason                     string `json:"reason"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		c, err := svc.AdminRequestClaim(ctx, partnermanager.ClaimOnBehalfRequest{ActorID: p.ActorID, PartnerID: input.PartnerID, AmountMinor: input.AmountMinor, ExpectedDestinationVersion: input.ExpectedDestinationVersion, IdempotencyKey: req.Key, Reason: input.Reason})
		if err != nil {
			return Response{}, err
		}
		accepted := acceptedOperatorClaim(c, p.ActorID, input.PartnerID, input.Reason, req.Key, input.AmountMinor, input.ExpectedDestinationVersion)
		if accepted == nil {
			return Response{}, partnerearnings.ErrUncertain
		}
		return reply(201, map[string]any{"claim": claimView(c, false), "accepted": accepted}, "")
	case "admin.partners.claims.decide":
		var input struct {
			ClaimID          string `json:"claim_id"`
			State            string `json:"state"`
			Reason           string `json:"reason"`
			ConfirmedUnsent  bool   `json:"confirmed_unsent"`
			ExpectedRevision int64  `json:"expected_revision"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		c, err := svc.AdminDecideClaim(ctx, partnerearnings.ClaimDecision{ActorID: p.ActorID, ClaimID: input.ClaimID, NewState: input.State, Reason: input.Reason, ConfirmedUnsent: input.ConfirmedUnsent, ExpectedRevision: input.ExpectedRevision})
		if err != nil {
			return Response{}, err
		}
		return reply(200, map[string]any{"claim": claimView(c, false), "assigned_to_you": c.ProcessingActor == p.ActorID, "new_handling_paused": !svc.ManualHandlingAdmitted()}, "")
	case "admin.partners.claims.observe":
		return partnersObservePayment(ctx, svc, p, req)
	case "admin.partners.claims.payment":
		return partnersRecordPayment(ctx, svc, p, req)
	case "admin.partners.claims.amend":
		var input struct {
			ClaimID          string `json:"claim_id"`
			Method           string `json:"method"`
			Reference        string `json:"reference"`
			PaidAt           string `json:"paid_at"`
			Reason           string `json:"reason"`
			ExpectedRevision int64  `json:"expected_revision"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		at, err := partnerDate(input.PaidAt)
		if err != nil {
			return Response{}, err
		}
		command := partnerearnings.AmendPaymentRequest{ActorID: p.ActorID, ClaimID: input.ClaimID, Method: input.Method, Reference: input.Reference, PaidAt: at, Reason: input.Reason, ExpectedRevision: input.ExpectedRevision, IdempotencyKey: req.Key}
		c, err := svc.AdminAmendPayment(ctx, command)
		if err != nil {
			return Response{}, err
		}
		return operatorReceipt(c, acceptedAmendment(c, command))
	case "admin.partners.claims.return":
		var input struct {
			ClaimID          string `json:"claim_id"`
			AmountMinor      int64  `json:"amount_minor"`
			Reference        string `json:"reference"`
			ReturnedAt       string `json:"returned_at"`
			Reason           string `json:"reason"`
			ExpectedRevision int64  `json:"expected_revision"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		at, err := partnerDate(input.ReturnedAt)
		if err != nil {
			return Response{}, err
		}
		command := partnerearnings.ReturnRequest{ActorID: p.ActorID, ClaimID: input.ClaimID, AmountMinor: input.AmountMinor, Currency: m.program.Currency, Reference: input.Reference, ReturnedAt: at, Reason: input.Reason, ExpectedRevision: input.ExpectedRevision, IdempotencyKey: req.Key}
		result, err := svc.AdminRecordReturnedTransfer(ctx, command)
		if err != nil {
			return Response{}, err
		}
		return operatorReceipt(result.Claim, acceptedReturn(result.Claim, command))
	case "admin.partners.status":
		var input struct {
			PartnerID           string `json:"partner_id"`
			Status              string `json:"status"`
			Reason              string `json:"reason"`
			ExpectedRevision    int64  `json:"expected_revision"`
			CanAcquireReferrals *bool  `json:"can_acquire_referrals"`
			CanAccrue           *bool  `json:"can_accrue"`
			CanRequestPayouts   *bool  `json:"can_request_payouts"`
		}
		if err := decode(req.Body, &input); err != nil {
			return Response{}, err
		}
		partner, err := svc.AdminChangeStatus(ctx, partnerprogram.StatusChangeRequest{ActorID: p.ActorID, PartnerID: input.PartnerID, NewStatus: input.Status, Reason: input.Reason, ExpectedRevision: input.ExpectedRevision, CanAcquireReferrals: input.CanAcquireReferrals, CanAccrue: input.CanAccrue, CanRequestPayouts: input.CanRequestPayouts})
		if err != nil {
			return Response{}, err
		}
		return reply(200, partnerView(partner), "")
	}
	return Response{}, fail("PARTNERS_INVALID_REQUEST", 400)
}

// Only canonical, independently known leaves can establish a denial/absence.
// A joined private storage failure is not a harmless wrapped not-found result.
func partnerUnknownCause(err error, depth int) bool {
	if err == nil {
		return false
	}
	if depth > 32 {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 || len(children) > 32 {
			return true
		}
		for _, child := range children {
			if partnerUnknownCause(child, depth+1) {
				return true
			}
		}
		return false
	}
	if child := errors.Unwrap(err); child != nil {
		return partnerUnknownCause(child, depth+1)
	}
	if host, ok := err.(*Error); ok {
		return !safePartnerHostError(host)
	}
	for _, known := range []error{partnermanager.ErrInvalid, partnermanager.ErrDenied, partnermanager.ErrIneligible, partnermanager.ErrNotFound, partnermanager.ErrNotPartner, partnermanager.ErrUnavailable, partnerprogram.ErrInvalid, partnerprogram.ErrDenied, partnerprogram.ErrNotFound, partnerprogram.ErrAlreadyEnrolled, partnerprogram.ErrAlreadyExists, partnerprogram.ErrUnavailable, partnerprogram.ErrUncertain, partnerprogram.ErrStaleWrite, partnerprogram.ErrAmbiguousPolicy, partnerearnings.ErrInvalid, partnerearnings.ErrDenied, partnerearnings.ErrNotFound, partnerearnings.ErrAlreadyExists, partnerearnings.ErrUnavailable, partnerearnings.ErrUncertain, partnerearnings.ErrReportTooLarge, partnerearnings.ErrUnresolved, partnerearnings.ErrCurrencyMismatch, partnerearnings.ErrStaleWrite, partnerearnings.ErrInsufficient, partnerearnings.ErrConflict, partnerearnings.ErrInvalidState, referral.ErrInvalid, referral.ErrDenied, referral.ErrNotFound, referral.ErrAlreadyExists, referral.ErrUnavailable, referral.ErrUncertain, referral.ErrStaleWrite, referral.ErrCapacity, referral.ErrSelfReferral, referral.ErrAlreadyReferred, referral.ErrCodeRetired, context.Canceled, context.DeadlineExceeded} {
		if err == known {
			return false
		}
	}
	return true
}

// safePartnerHostError reports whether a transport Error may pass through
// unchanged: only host-produced invalid-request(400), auth-required(401),
// verification/account(403) and dependency-unavailable(503) pairs are allowed.
// Every other code/status combination is treated as unsafe.
func safePartnerHostError(e *Error) bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case "PARTNERS_INVALID_REQUEST":
		return e.Status == 400
	case "PARTNERS_AUTH_REQUIRED":
		return e.Status == 401
	case "PARTNERS_VERIFICATION_REQUIRED", "PARTNERS_ACCOUNT_UNAVAILABLE":
		return e.Status == 403
	case "PARTNERS_DEPENDENCY_UNAVAILABLE":
		return e.Status == 503
	}
	return false
}
