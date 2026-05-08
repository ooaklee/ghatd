package pricer

import "github.com/ooaklee/reply/v2"

// PricerErrorMap holds error keys, their corresponding human-friendly message, and response status code.
var PricerErrorMap reply.ErrorManifest = reply.ErrorManifest{
	ErrInvalidPricePlanPayload:    {Title: "Bad Request", Detail: "Invalid price plan payload", StatusCode: 400, Code: "PRC0-01"},
	ErrInvalidPriceFeaturePayload: {Title: "Bad Request", Detail: "Invalid price feature payload", StatusCode: 400, Code: "PRC0-02"},
	ErrInvalidPricePlanStatus:     {Title: "Bad Request", Detail: "Invalid price plan status", StatusCode: 400, Code: "PRC0-03"},
	ErrInvalidPriceFeatureType:    {Title: "Bad Request", Detail: "Invalid price feature type", StatusCode: 400, Code: "PRC0-04"},
	ErrInvalidPriceFeatureUnit:    {Title: "Bad Request", Detail: "Invalid price feature unit", StatusCode: 400, Code: "PRC0-05"},
	ErrInvalidPriceBillingCadence: {Title: "Bad Request", Detail: "Invalid price billing cadence", StatusCode: 400, Code: "PRC0-06"},
	ErrInvalidPriceProvider:       {Title: "Bad Request", Detail: "Invalid price provider", StatusCode: 400, Code: "PRC0-07"},
	ErrInvalidPriceSlug:           {Title: "Bad Request", Detail: "Invalid price slug", StatusCode: 400, Code: "PRC0-08"},
	ErrInvalidPriceCost:           {Title: "Bad Request", Detail: "Invalid price cost", StatusCode: 400, Code: "PRC0-09"},
	ErrInvalidPriceCurrency:       {Title: "Bad Request", Detail: "Invalid price currency", StatusCode: 400, Code: "PRC0-10"},
	ErrDuplicatePlanFeatureRef:    {Title: "Bad Request", Detail: "Duplicate plan feature reference", StatusCode: 400, Code: "PRC0-11"},
	ErrMissingPlanFeatureRef:      {Title: "Bad Request", Detail: "Missing plan feature reference", StatusCode: 400, Code: "PRC0-12"},
	ErrPricePlanNotFound:          {Title: "Not Found", Detail: "Price plan not found", StatusCode: 404, Code: "PRC0-13"},
	ErrPriceFeatureNotFound:       {Title: "Not Found", Detail: "Price feature not found", StatusCode: 404, Code: "PRC0-14"},
	ErrPriceUserIDRequired:        {Title: "Forbidden", Detail: "User ID is required", StatusCode: 403, Code: "PRC0-15"},
	ErrPricePlanIDRequired:        {Title: "Bad Request", Detail: "Price plan ID is required", StatusCode: 400, Code: "PRC0-16"},
	ErrPriceFeatureIDRequired:     {Title: "Bad Request", Detail: "Price feature ID is required", StatusCode: 400, Code: "PRC0-17"},
	ErrPricePlanPublishRequiresCost: {
		Title:      "Bad Request",
		Detail:     "Published price plans require at least one valid cost",
		StatusCode: 400,
		Code:       "PRC0-18",
	},
	ErrPricePlanPublishRequiresProvider: {
		Title:      "Bad Request",
		Detail:     "Published price plans require at least one provider reference",
		StatusCode: 400,
		Code:       "PRC0-19",
	},
	ErrInvalidPriceQueryParam:   {Title: "Bad Request", Detail: "Invalid price query parameter", StatusCode: 400, Code: "PRC0-20"},
	ErrInvalidPriceDate:         {Title: "Bad Request", Detail: "Invalid price date value", StatusCode: 400, Code: "PRC0-21"},
	ErrInvalidPriceDiscount:     {Title: "Bad Request", Detail: "Invalid price discount", StatusCode: 400, Code: "PRC0-22"},
	ErrInvalidPricePaymentTerms: {Title: "Bad Request", Detail: "Invalid price payment terms", StatusCode: 400, Code: "PRC0-23"},
}
