package billinglifecycle

import (
	"fmt"
	"github.com/ooaklee/ghatd/external/billing"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// RuntimeConfig supplies explicit provider scopes, service identity and bounded
// scheduling policy. It creates no identity or grants and starts no work.
type RuntimeConfig struct {
	ActorID                                                    string
	Scopes                                                     []billing.RevenueScope
	PageSize, ColdBudget, RefreshBudget                        int
	Interval, PassTimeout, Lease, RetryBase, RetryMax, Cadence time.Duration
}

func (c RuntimeConfig) ValidBounds() bool {
	return c.PageSize >= 1 && c.PageSize <= 200 && c.ColdBudget >= 1 && c.ColdBudget <= c.PageSize && c.RefreshBudget >= 1 && c.RefreshBudget <= c.PageSize && c.Interval >= time.Second && c.Interval <= time.Hour && c.PassTimeout >= time.Second && c.PassTimeout <= 15*time.Minute && c.Lease >= time.Second && c.Lease <= 15*time.Minute && c.RetryBase >= time.Second && c.RetryBase <= time.Minute && c.RetryMax >= c.RetryBase && c.RetryMax <= 24*time.Hour && c.Cadence >= time.Second && c.Cadence <= 24*time.Hour
}
func (c RuntimeConfig) Validate() error {
	if !validRuntimeActor(c.ActorID) || billing.ValidateRevenueHistoryScopes(c.Scopes) != nil || !c.ValidBounds() {
		return fmt.Errorf("billinglifecycle/runtime-configuration-invalid")
	}
	return nil
}

func validRuntimeActor(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}
