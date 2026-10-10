package partnerruntime

import (
	"time"

	"github.com/ooaklee/ghatd/external/billinglifecycle"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// Config contains trusted startup data only. It owns no environment parsing,
// credentials, routes or activation decisions. A host supplies every commercial
// control explicitly; construction never enables or starts a worker.
// ReservedKeys lists keys for other host purposes, which must not be reused for
// payload encryption or any retained referral signing key.
type Config struct {
	Lifecycle        *billinglifecycle.RuntimeConfig
	Program          partnerprogram.Config
	PayloadKey       []byte
	ReservedKeys     [][]byte
	Evidence         referral.EvidenceConfig
	VisitWindow      time.Duration
	Analytics        *referral.AnalyticsConfig
	Controls         partnermanager.Controls
	Claims           partnermanager.ClaimsConfig
	Queue            partnermanager.WorkQueueConfig
	RevenueReporting *partnermanager.RevenueReportingConfig
	RevenueCapture   bool
	Worker           *WorkerConfig
}

// WorkerConfig supplies trusted service identity, batch bounds and host cadence.
// It does not start a worker; the owning worker validates its Native config.
type WorkerConfig struct {
	Native   partnermanager.WorkerConfig
	Interval time.Duration
}
