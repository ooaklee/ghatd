package pricer

import "github.com/ooaklee/reply"

// PricerErrorMap holds error keys, their corresponding human-friendly message, and response status code.
var PricerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrKeyInvalidPricePlanPayload:    {Title: "Bad Request", Detail: "Invalid price plan payload", StatusCode: 400, Code: "PRC0-01"},
	ErrKeyInvalidPriceFeaturePayload: {Title: "Bad Request", Detail: "Invalid price feature payload", StatusCode: 400, Code: "PRC0-02"},
	ErrKeyInvalidPricePlanStatus:     {Title: "Bad Request", Detail: "Invalid price plan status", StatusCode: 400, Code: "PRC0-03"},
	ErrKeyInvalidPriceFeatureType:    {Title: "Bad Request", Detail: "Invalid price feature type", StatusCode: 400, Code: "PRC0-04"},
	ErrKeyInvalidPriceFeatureUnit:    {Title: "Bad Request", Detail: "Invalid price feature unit", StatusCode: 400, Code: "PRC0-05"},
	ErrKeyInvalidPriceBillingCadence: {Title: "Bad Request", Detail: "Invalid price billing cadence", StatusCode: 400, Code: "PRC0-06"},
	ErrKeyInvalidPriceProvider:       {Title: "Bad Request", Detail: "Invalid price provider", StatusCode: 400, Code: "PRC0-07"},
	ErrKeyInvalidPriceSlug:           {Title: "Bad Request", Detail: "Invalid price slug", StatusCode: 400, Code: "PRC0-08"},
	ErrKeyInvalidPriceCost:           {Title: "Bad Request", Detail: "Invalid price cost", StatusCode: 400, Code: "PRC0-09"},
	ErrKeyInvalidPriceCurrency:       {Title: "Bad Request", Detail: "Invalid price currency", StatusCode: 400, Code: "PRC0-10"},
	ErrKeyDuplicatePlanFeatureRef:    {Title: "Bad Request", Detail: "Duplicate plan feature reference", StatusCode: 400, Code: "PRC0-11"},
	ErrKeyMissingPlanFeatureRef:      {Title: "Bad Request", Detail: "Missing plan feature reference", StatusCode: 400, Code: "PRC0-12"},
}
