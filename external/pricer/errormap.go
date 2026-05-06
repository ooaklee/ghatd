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
	ErrKeyPricePlanNotFound:          {Title: "Not Found", Detail: "Price plan not found", StatusCode: 404, Code: "PRC0-13"},
	ErrKeyPriceFeatureNotFound:       {Title: "Not Found", Detail: "Price feature not found", StatusCode: 404, Code: "PRC0-14"},
	ErrKeyPriceUserIDRequired:        {Title: "Forbidden", Detail: "User ID is required", StatusCode: 403, Code: "PRC0-15"},
	ErrKeyPricePlanIDRequired:        {Title: "Bad Request", Detail: "Price plan ID is required", StatusCode: 400, Code: "PRC0-16"},
	ErrKeyPriceFeatureIDRequired:     {Title: "Bad Request", Detail: "Price feature ID is required", StatusCode: 400, Code: "PRC0-17"},
	ErrKeyPricePlanPublishRequiresCost: {
		Title:      "Bad Request",
		Detail:     "Published price plans require at least one valid cost",
		StatusCode: 400,
		Code:       "PRC0-18",
	},
	ErrKeyPricePlanPublishRequiresProvider: {
		Title:      "Bad Request",
		Detail:     "Published price plans require at least one provider reference",
		StatusCode: 400,
		Code:       "PRC0-19",
	},
	ErrKeyInvalidPriceQueryParam: {Title: "Bad Request", Detail: "Invalid price query parameter", StatusCode: 400, Code: "PRC0-20"},
	ErrKeyInvalidPriceDate:       {Title: "Bad Request", Detail: "Invalid price date value", StatusCode: 400, Code: "PRC0-21"},
}
