package policy

import "errors"

var (
	ErrInvalidpolicyName = errors.New(ErrKeyInvalidpolicyName)
	ErrPolicyError       = errors.New(ErrKeyPolicyError)
	ErrPolicyNotFound    = errors.New(ErrKeyPolicyNotFound)
)
