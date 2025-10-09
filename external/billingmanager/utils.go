package billingmanager

import (
	"fmt"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// parseTimeOrNil attempts to parse a time string into a time.Time pointer.
func parseTimeOrNil(timeStr string) *time.Time {
	if timeStr == "" {
		return nil
	}

	// Try various time formats
	formats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			return &t
		}
	}

	return nil
}

// formatEventDescription formats event description based on event type, plan name, and status
func formatEventDescription(eventType, planName, status string) string {
	switch eventType {
	case paymentprovider.EventTypePaymentSucceeded:
		if status == billing.StatusTrialing {
			return fmt.Sprintf("Trial started for %s", planName)
		}
		return fmt.Sprintf("Payment successful for %s", planName)
	case paymentprovider.EventTypePaymentFailed:
		return fmt.Sprintf("Payment failed for %s", planName)
	case paymentprovider.EventTypePaymentRefunded:
		return fmt.Sprintf("Payment refunded for %s", planName)
	case paymentprovider.EventTypeSubscriptionCreated:
		return fmt.Sprintf("Subscription created: %s", planName)
	case paymentprovider.EventTypeSubscriptionCancelled:
		return fmt.Sprintf("Subscription cancelled: %s", planName)
	case paymentprovider.EventTypeSubscriptionUpdated:
		return fmt.Sprintf("Subscription updated: %s", planName)
	default:
		return fmt.Sprintf("%s - %s", eventType, planName)
	}
}
