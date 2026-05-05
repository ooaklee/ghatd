package ephemeral

const (

	// ErrKeyRequestorLimitExceeded error occurs when the requester exceed the set limit
	ErrKeyRequestorLimitExceeded string = "RequestorLimitExceeded"

	// ErrKeyHardenedRateLimitExceeded error occurs when the requester exceeds the hardened rate limit for code verification
	ErrKeyHardenedRateLimitExceeded string = "HardenedRateLimitExceeded"
)
