package paymentprovider

import "errors"

var (
	ErrPaymentProviderAPIRequestFailed               = errors.New(ErrKeyPaymentProviderAPIRequestFailed)
	ErrPaymentProviderAPIResponseInvalid             = errors.New(ErrKeyPaymentProviderAPIResponseInvalid)
	ErrPaymentProviderInvalidConfigWebhookSecret     = errors.New(ErrKeyPaymentProviderInvalidConfigWebhookSecret)
	ErrPaymentProviderInvalidConfiguration           = errors.New(ErrKeyPaymentProviderInvalidConfiguration)
	ErrPaymentProviderInvalidEventType               = errors.New(ErrKeyPaymentProviderInvalidEventType)
	ErrPaymentProviderInvalidPayload                 = errors.New(ErrKeyPaymentProviderInvalidPayload)
	ErrPaymentProviderInvalidWebhookSignature        = errors.New(ErrKeyPaymentProviderInvalidWebhookSignature)
	ErrPaymentProviderKofiNoSubscriptionAPI          = errors.New(ErrKeyPaymentProviderKofiNoSubscriptionAPI)
	ErrPaymentProviderMissingConfiguration           = errors.New(ErrKeyPaymentProviderMissingConfiguration)
	ErrPaymentProviderMissingPayloadCustomerEmail    = errors.New(ErrKeyPaymentProviderMissingPayloadCustomerEmail)
	ErrPaymentProviderMissingRequiredField           = errors.New(ErrKeyPaymentProviderMissingRequiredField)
	ErrPaymentProviderMissingSignature               = errors.New(ErrKeyPaymentProviderMissingSignature)
	ErrPaymentProviderNotFound                       = errors.New(ErrKeyPaymentProviderNotFound)
	ErrPaymentProviderPayloadParsing                 = errors.New(ErrKeyPaymentProviderPayloadParsing)
	ErrPaymentProviderRequiredWebhookSecretIsMissing = errors.New(ErrKeyPaymentProviderRequiredWebhookSecretIsMissing)
	ErrPaymentProviderSubscriptionNotFound           = errors.New(ErrKeyPaymentProviderSubscriptionNotFound)
	ErrPaymentProviderUnsupportedProvider            = errors.New(ErrKeyPaymentProviderUnsupportedProvider)
	ErrPaymentProviderWebhookTimestampTooOld         = errors.New(ErrKeyPaymentProviderWebhookTimestampTooOld)
)
