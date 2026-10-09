package helpers

import (
	"encoding/json"
	"strings"
	"time"
)

// StripePaidServicePeriodConfig binds parsing to trusted persisted billing IDs
// and the host's configured price/currency/quantity/mode. No input verifies a
// signature or authorises an entitlement by itself.
type StripePaidServicePeriodConfig struct {
	// ExpectedEventID is the already-persisted native Stripe event identifier.
	ExpectedEventID string
	// SubscriptionID and CustomerID bind the invoice to an owning subscription.
	SubscriptionID, CustomerID string
	// PriceID and Currency must match the host's configured binding.
	PriceID, Currency string
	// Quantity is the required positive line quantity.
	Quantity int
	// LiveMode must match the trusted environment/provider configuration.
	LiveMode bool
}

// StripePaidServicePeriod describes one complete paid renewal line with UTC
// timestamps. Hosts retain cadence/duration rules, quotas, grant IDs and policy.
type StripePaidServicePeriod struct {
	// Native IDs retain their exact values from the qualifying invoice.
	InvoiceID, SubscriptionID, CustomerID, PriceID string
	// Currency retains the payload spelling after case-insensitive matching.
	Currency string
	// Quantity matches the required positive configured quantity.
	Quantity int
	// StartsAt and ExpiresAt are UTC; start is positive and end is later.
	StartsAt, ExpiresAt time.Time
}

// ParseStripePaidServicePeriod checks invoice.paid/payment_succeeded envelope
// identity, mode, paid status, full single renewal line, non-proration and exact
// expected IDs/quantity/currency. Legacy and modern Stripe fields are supported.
// It performs no I/O/signature verification or financial writes; call only on
// trusted persisted evidence after the owning webhook verification boundary.
// Malformed/non-qualifying payloads return ErrStripePaidServicePeriodInvalid;
// missing expectations return ErrStripePaidServicePeriodConfigInvalid.
func ParseStripePaidServicePeriod(raw []byte, cfg StripePaidServicePeriodConfig) (StripePaidServicePeriod, error) {
	if cfg.ExpectedEventID == "" || cfg.SubscriptionID == "" || cfg.CustomerID == "" || cfg.PriceID == "" || cfg.Currency == "" || cfg.Quantity <= 0 {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodConfigInvalid
	}
	var envelope struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Livemode bool   `json:"livemode"`
		Data     struct {
			Object struct {
				ID            string         `json:"id"`
				Object        string         `json:"object"`
				Status        string         `json:"status"`
				Paid          *bool          `json:"paid"`
				Currency      string         `json:"currency"`
				BillingReason string         `json:"billing_reason"`
				Subscription  stripePeriodID `json:"subscription"`
				Customer      stripePeriodID `json:"customer"`
				Parent        struct {
					SubscriptionDetails struct {
						Subscription stripePeriodID `json:"subscription"`
					} `json:"subscription_details"`
				} `json:"parent"`
				Lines struct {
					HasMore bool               `json:"has_more"`
					Data    []stripePeriodLine `json:"data"`
				} `json:"lines"`
			} `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.ID != cfg.ExpectedEventID || envelope.Livemode != cfg.LiveMode || (envelope.Type != "invoice.paid" && envelope.Type != "invoice.payment_succeeded") {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodInvalid
	}
	invoice := envelope.Data.Object
	subscription := invoice.Subscription
	if subscription == "" {
		subscription = invoice.Parent.SubscriptionDetails.Subscription
	}
	if invoice.Object != "invoice" || !strings.HasPrefix(invoice.ID, "in_") || invoice.Status != "paid" || (invoice.Paid != nil && !*invoice.Paid) || string(subscription) != cfg.SubscriptionID || string(invoice.Customer) != cfg.CustomerID || !strings.EqualFold(invoice.Currency, cfg.Currency) {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodInvalid
	}
	// Only full, paid renewal periods refill quota. A proration or a duplicate
	// payment event must never mint another full allowance.
	if invoice.BillingReason != "subscription_create" && invoice.BillingReason != "subscription_cycle" {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodInvalid
	}
	if invoice.Lines.HasMore || len(invoice.Lines.Data) != 1 {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodInvalid
	}
	line := invoice.Lines.Data[0]
	price := line.Price
	if price == "" {
		price = line.Pricing.PriceDetails.Price
	}
	if string(price) != cfg.PriceID || line.Quantity != cfg.Quantity || line.Proration || line.Parent.SubscriptionItemDetails.Proration {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodInvalid
	}
	if lineSub := line.Parent.SubscriptionItemDetails.Subscription; lineSub != "" && string(lineSub) != cfg.SubscriptionID {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodInvalid
	}
	start, end := time.Unix(line.Period.Start, 0).UTC(), time.Unix(line.Period.End, 0).UTC()
	if line.Period.Start <= 0 || line.Period.End <= line.Period.Start {
		return StripePaidServicePeriod{}, ErrStripePaidServicePeriodInvalid
	}
	return StripePaidServicePeriod{InvoiceID: invoice.ID, SubscriptionID: string(subscription), CustomerID: string(invoice.Customer), PriceID: string(price), Currency: invoice.Currency, Quantity: line.Quantity, StartsAt: start, ExpiresAt: end}, nil
}

type stripePeriodID string

func (id *stripePeriodID) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*id = stripePeriodID(text)
		return nil
	}
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	*id = stripePeriodID(object.ID)
	return nil
}

type stripePeriodLine struct {
	Price     stripePeriodID `json:"price"`
	Quantity  int            `json:"quantity"`
	Proration bool           `json:"proration"`
	Period    struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"period"`
	Pricing struct {
		PriceDetails struct {
			Price stripePeriodID `json:"price"`
		} `json:"price_details"`
	} `json:"pricing"`
	Parent struct {
		SubscriptionItemDetails struct {
			Subscription stripePeriodID `json:"subscription"`
			Proration    bool           `json:"proration"`
		} `json:"subscription_item_details"`
	} `json:"parent"`
}
