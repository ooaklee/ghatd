package paymentprovider

import (
	"context"
	"encoding/json"
	"math/big"
	"net/url"
	"strings"
)

// populateStripeRevenueRefunds requires the immutable signed cumulative amount
// and authenticated refunds to agree. Full refunds reverse the proven original
// net. Partial refunds additionally require complete refund-linked credit-note
// line allocations; no proportional net/tax estimate is made.
func (s *StripeProvider) populateStripeRevenueRefunds(ctx context.Context, req RevenueInvoiceRequest, intent map[string]json.RawMessage, invoice *RevenueInvoiceEvidence) error {
	chargeID := rawStripeID(intent["latest_charge"])
	if !stripeRevenueObjectID.MatchString(chargeID) || req.ExpectedCumulativeRefundedGrossMinor <= 0 {
		return ErrRevenueUnassessable
	}
	charge, err := s.revenueGet(ctx, req.Scope, "/v1/charges/"+url.PathEscape(chargeID))
	if err != nil {
		return err
	}
	amount, ok := rawStripeInt(charge["amount"])
	refunded, known := rawStripeInt(charge["amount_refunded"])
	if !ok || !known || amount != invoice.GrossPaidMinor || refunded != req.ExpectedCumulativeRefundedGrossMinor || refunded > amount || rawStripeID(charge["id"]) != chargeID || rawStripeID(charge["payment_intent"]) != invoice.PaymentID || rawStripeID(charge["customer"]) != invoice.CustomerID || !rawStripeMode(charge, req.Scope) || strings.ToUpper(rawStripeString(charge["currency"])) != invoice.Currency {
		return ErrRevenueUnassessable
	}
	refunds, err := s.revenueList(ctx, req.Scope, "/v1/refunds?charge="+url.QueryEscape(chargeID))
	if err != nil {
		return err
	}
	refundAmounts := map[string]int64{}
	var gross int64
	for _, refund := range refunds {
		if rawStripeString(refund["status"]) != "succeeded" {
			continue
		}
		amount, known := rawStripeInt(refund["amount"])
		if !known || amount <= 0 || rawStripeID(refund["charge"]) != chargeID || rawStripeID(refund["payment_intent"]) != invoice.PaymentID || strings.ToUpper(rawStripeString(refund["currency"])) != invoice.Currency {
			return ErrRevenueUnassessable
		}
		// Stripe Refund objects omit livemode. Their authenticated, charge-scoped
		// list and exact original charge/payment links bind them to the parent
		// mode verified above. Reject any contradictory or malformed extra field;
		// absence is not permission to omit mode on invoices, charges or notes.
		if _, supplied := refund["livemode"]; supplied && !rawStripeMode(refund, req.Scope) {
			return ErrRevenueUnassessable
		}
		gross, err = revenueMath(gross, amount)
		if err != nil {
			return err
		}
		refundAmounts[rawStripeID(refund["id"])] = amount
	}
	if gross != refunded {
		return ErrRevenueUnassessable
	}
	if refunded == invoice.GrossPaidMinor {
		for i := range invoice.Lines {
			invoice.Lines[i].CumulativeRefundedMinor = invoice.Lines[i].NetPaidMinor
		}
		return nil
	}
	notes, err := s.revenueList(ctx, req.Scope, "/v1/credit_notes?invoice="+url.QueryEscape(invoice.InvoiceID))
	if err != nil {
		return err
	}
	lineAmounts := map[string]int64{}
	refundCovered := map[string]int64{}
	for _, note := range notes {
		if rawStripeString(note["status"]) == "void" {
			continue
		}
		if rawStripeString(note["status"]) != "issued" || rawStripeString(note["type"]) != "post_payment" || rawStripeID(note["invoice"]) != invoice.InvoiceID || !rawStripeMode(note, req.Scope) || strings.ToUpper(rawStripeString(note["currency"])) != invoice.Currency {
			return ErrRevenueUnassessable
		}
		pre, ok := rawStripeInt(note["pre_payment_amount"])
		post, known := rawStripeInt(note["post_payment_amount"])
		net, netKnown := rawStripeInt(note["total_excluding_tax"])
		if !ok || pre != 0 || !known || post <= 0 || !netKnown || net < 0 || net > post {
			return ErrRevenueUnassessable
		}
		var links []struct {
			Refund json.RawMessage `json:"refund"`
			Amount *int64          `json:"amount_refunded"`
		}
		if json.Unmarshal(note["refunds"], &links) != nil || len(links) == 0 {
			return ErrRevenueUnassessable
		}
		var linked int64
		for _, link := range links {
			id := rawStripeID(link.Refund)
			available, ok := refundAmounts[id]
			if !ok || link.Amount == nil || *link.Amount <= 0 {
				return ErrRevenueUnassessable
			}
			refundCovered[id], err = revenueMath(refundCovered[id], *link.Amount)
			if err != nil || refundCovered[id] > available {
				return ErrRevenueUnassessable
			}
			linked, err = revenueMath(linked, *link.Amount)
			if err != nil {
				return err
			}
		}
		if linked != post {
			return ErrRevenueUnassessable
		}
		lines, err := s.revenueList(ctx, req.Scope, "/v1/credit_notes/"+url.PathEscape(rawStripeID(note["id"]))+"/lines")
		if err != nil {
			return err
		}
		var lineSum int64
		for _, line := range lines {
			id := rawStripeID(line["invoice_line_item"])
			if id == "" || rawStripeString(line["type"]) != "invoice_line_item" || !rawStripeMode(line, req.Scope) {
				return ErrRevenueUnassessable
			}
			amount, err := stripeCreditLineNet(line)
			if err != nil {
				return err
			}
			lineSum, err = revenueMath(lineSum, amount)
			if err != nil {
				return err
			}
			lineAmounts[id], err = revenueMath(lineAmounts[id], amount)
			if err != nil {
				return err
			}
		}
		if lineSum != net {
			return ErrRevenueUnassessable
		}
	}
	for id, amount := range refundAmounts {
		if refundCovered[id] != amount {
			return ErrRevenueUnassessable
		}
	}
	for i, line := range invoice.Lines {
		amount := lineAmounts[line.ID]
		if amount > line.NetPaidMinor {
			return ErrRevenueUnassessable
		}
		invoice.Lines[i].CumulativeRefundedMinor = amount
		delete(lineAmounts, line.ID)
	}
	if len(lineAmounts) > 0 {
		return ErrRevenueUnassessable
	}
	return nil
}

func stripeCreditLineNet(line map[string]json.RawMessage) (int64, error) {
	base, ok := rawStripeInt(line["amount"])
	if !ok || base < 0 {
		return 0, ErrRevenueUnassessable
	}
	// Credit-note amount includes inclusive tax but excludes exclusive tax and
	// discounts. Remove exactly one credit representation and inclusive tax,
	// then reconcile all calculated lines with the note's total_excluding_tax.
	copyLine := map[string]json.RawMessage{}
	for k, v := range line {
		copyLine[k] = v
	}
	copyLine["subtotal"] = line["amount"]
	net, err := stripeRevenueLineNet(copyLine)
	if err != nil {
		return 0, err
	}
	sum := big.NewInt(net)
	taxes := line["taxes"]
	if len(taxes) == 0 {
		taxes = line["tax_amounts"]
	}
	if len(taxes) == 0 {
		return 0, ErrRevenueUnassessable
	}
	var values []struct {
		Amount      *int64 `json:"amount"`
		TaxBehavior string `json:"tax_behavior"`
		Inclusive   *bool  `json:"inclusive"`
	}
	if json.Unmarshal(taxes, &values) != nil {
		return 0, ErrRevenueUnassessable
	}
	for _, tax := range values {
		if tax.Amount == nil || *tax.Amount < 0 {
			return 0, ErrRevenueUnassessable
		}
		inclusive := tax.TaxBehavior == "inclusive"
		if tax.TaxBehavior == "" && tax.Inclusive != nil {
			inclusive = *tax.Inclusive
		} else if tax.TaxBehavior != "inclusive" && tax.TaxBehavior != "exclusive" {
			return 0, ErrRevenueUnassessable
		}
		if inclusive {
			sum.Sub(sum, big.NewInt(*tax.Amount))
		}
	}
	if !sum.IsInt64() || sum.Sign() < 0 {
		return 0, ErrRevenueUnassessable
	}
	return sum.Int64(), nil
}
