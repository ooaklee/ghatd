package user

import (
	"errors"
	"maps"
	"slices"
)

// ErrUserUpdateUnavailable rejects incomplete wiring and invalid write receipts.
// A failed write can have committed; callers must not infer rollback or retry it.
var ErrUserUpdateUnavailable = errors.New("UserUpdateUnavailable")

// copyUserForUpdate isolates fields mutated by dependency injection, model
// methods and repository adapters. Arbitrary nested extension values remain
// read-only by contract; cloning them by JSON/BSON would change their Go types.
// Configuration and utility dependencies are shared startup-only collaborators.
func copyUserForUpdate(source *UniversalUser) *UniversalUser {
	if source == nil {
		return nil
	}
	value := *source
	value.Roles = slices.Clone(source.Roles)
	value.OAuthIdentities = slices.Clone(source.OAuthIdentities)
	value.OAuthIdentityKeys = slices.Clone(source.OAuthIdentityKeys)
	value.Extensions = maps.Clone(source.Extensions)
	if source.PersonalInfo != nil {
		personal := *source.PersonalInfo
		value.PersonalInfo = &personal
	}
	if source.Verification != nil {
		verification := *source.Verification
		value.Verification = &verification
	}
	if source.Metadata != nil {
		metadata := *source.Metadata
		metadata.CustomTimestamps = maps.Clone(source.Metadata.CustomTimestamps)
		value.Metadata = &metadata
	}
	if source.HandleMetadata != nil {
		handle := *source.HandleMetadata
		value.HandleMetadata = &handle
	}
	return &value
}
