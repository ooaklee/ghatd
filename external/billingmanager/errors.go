package billingmanager

import "errors"

var (
	ErrBillingManagerFailedToProcessEvent                  = errors.New(ErrKeyBillingManagerFailedToProcessEvent)
	ErrBillingManagerFailedToRetrieveBillingEvents         = errors.New(ErrKeyBillingManagerFailedToRetrieveBillingEvents)
	ErrBillingManagerFailedToRetrieveSubscriptionStatus    = errors.New(ErrKeyBillingManagerFailedToRetrieveSubscriptionStatus)
	ErrBillingManagerFailedWebhookVerification             = errors.New(ErrKeyBillingManagerFailedWebhookVerification)
	ErrBillingManagerNoUserIdentifyingInformationInPayload = errors.New(ErrKeyBillingManagerNoUserIdentifyingInformationInPayload)
	ErrBillingManagerPricerServiceNotSet                   = errors.New(ErrKeyBillingManagerPricerServiceNotSet)
	ErrBillingManagerRequiresUserIdIsMissing               = errors.New(ErrKeyBillingManagerRequiresUserIdIsMissing)
	ErrBillingManagerUnableToGetProviderNameFromURI        = errors.New(ErrKeyBillingManagerUnableToGetProviderNameFromURI)
	ErrBillingManagerUnableToGetUserIdFromURI              = errors.New(ErrKeyBillingManagerUnableToGetUserIdFromURI)
	ErrBillingManagerUnableToIdentifyUser                  = errors.New(ErrKeyBillingManagerUnableToIdentifyUser)
	ErrBillingManagerUnableToResolveUserId                 = errors.New(ErrKeyBillingManagerUnableToResolveUserId)
	ErrBillingManagerUserUnauthorisedToCarryOutOperation   = errors.New(ErrKeyBillingManagerUserUnauthorisedToCarryOutOperation)
	ErrInvalidBillingManagerRequestPayload                 = errors.New(ErrKeyInvalidBillingManagerRequestPayload)
)
