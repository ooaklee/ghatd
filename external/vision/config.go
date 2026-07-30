package vision

import (
	"fmt"
	"slices"
)

// VisionConfig controls vision types, roadmap transitions, and voting.
//
// StatusTransitions follows the group package convention: each map key is a
// destination status and its slice contains the statuses it may transition
// from. The empty string represents a feedback/bug item that is not yet on the
// roadmap.
type VisionConfig struct {
	StatusTransitions map[VisionStatus][]VisionStatus
	ValidTypes        []VisionType
	EnableDownvoting  bool
}

// DefaultVisionConfig returns the default feedback and roadmap behaviour.
func DefaultVisionConfig() *VisionConfig {
	return &VisionConfig{
		StatusTransitions: map[VisionStatus][]VisionStatus{
			VisionStatusUnderReview: {
				"",
				VisionStatusRejected,
			},
			VisionStatusPlanning: {
				VisionStatusUnderReview,
			},
			VisionStatusPlanned: {
				VisionStatusUnderReview,
				VisionStatusPlanning,
			},
			VisionStatusRejected: {
				VisionStatusUnderReview,
				VisionStatusPlanning,
				VisionStatusPlanned,
			},
			VisionStatusInProgress: {
				VisionStatusPlanning,
				VisionStatusPlanned,
			},
			VisionStatusComplete: {
				VisionStatusInProgress,
			},
		},
		ValidTypes: []VisionType{
			VisionTypeBugs,
			VisionTypeFeedback,
		},
		EnableDownvoting: true,
	}
}

// NewCustomVisionConfig creates an empty config for host applications to
// build up.
func NewCustomVisionConfig() *VisionConfig {
	return &VisionConfig{
		StatusTransitions: make(map[VisionStatus][]VisionStatus),
		ValidTypes:        []VisionType{},
	}
}

// WithStatusTransition sets the allowed source statuses for a destination.
func (c *VisionConfig) WithStatusTransition(to VisionStatus, from ...VisionStatus) *VisionConfig {
	if c.StatusTransitions == nil {
		c.StatusTransitions = make(map[VisionStatus][]VisionStatus)
	}
	normalisedSources := make([]VisionStatus, len(from))
	for i := range from {
		normalisedSources[i] = normaliseVisionStatus(from[i])
	}
	c.StatusTransitions[normaliseVisionStatus(to)] = normalisedSources
	return c
}

// WithValidTypes replaces the set of accepted vision types.
func (c *VisionConfig) WithValidTypes(types ...VisionType) *VisionConfig {
	c.ValidTypes = make([]VisionType, len(types))
	for i := range types {
		c.ValidTypes[i] = normaliseVisionType(types[i])
	}
	return c
}

// WithDownvoting enables or disables downvotes.
func (c *VisionConfig) WithDownvoting(enabled bool) *VisionConfig {
	c.EnableDownvoting = enabled
	return c
}

// IsValidType reports whether a type is enabled by the config.
func (c *VisionConfig) IsValidType(visionType VisionType) bool {
	if c == nil {
		return false
	}
	return slices.Contains(c.ValidTypes, normaliseVisionType(visionType))
}

// IsValidStatus reports whether a status is part of the transition graph.
func (c *VisionConfig) IsValidStatus(status VisionStatus) bool {
	if c == nil {
		return false
	}
	_, ok := c.StatusTransitions[normaliseVisionStatus(status)]
	return ok
}

// Validate verifies that the config has reachable statuses and valid types.
func (c *VisionConfig) Validate() error {
	if c == nil || len(c.ValidTypes) == 0 || len(c.StatusTransitions) == 0 {
		return ErrVisionConfigInvalid
	}

	seenTypes := make(map[VisionType]struct{}, len(c.ValidTypes))
	normalisedTypes := make([]VisionType, len(c.ValidTypes))
	for i, visionType := range c.ValidTypes {
		normalised := normaliseVisionType(visionType)
		if normalised == "" {
			return fmt.Errorf("%w: empty vision type", ErrVisionConfigInvalid)
		}
		if _, exists := seenTypes[normalised]; exists {
			return fmt.Errorf("%w: duplicate vision type %q", ErrVisionConfigInvalid, normalised)
		}
		seenTypes[normalised] = struct{}{}
		normalisedTypes[i] = normalised
	}

	normalisedTransitions := make(map[VisionStatus][]VisionStatus, len(c.StatusTransitions))
	for destination, sources := range c.StatusTransitions {
		destination = normaliseVisionStatus(destination)
		if destination == "" {
			return fmt.Errorf("%w: empty destination status", ErrVisionConfigInvalid)
		}
		if _, exists := normalisedTransitions[destination]; exists {
			return fmt.Errorf("%w: duplicate destination status %q", ErrVisionConfigInvalid, destination)
		}
		normalisedSources := make([]VisionStatus, len(sources))
		for i, source := range sources {
			normalisedSources[i] = normaliseVisionStatus(source)
		}
		normalisedTransitions[destination] = normalisedSources
	}

	hasInitialTransition := false
	for _, sources := range normalisedTransitions {
		for _, source := range sources {
			if source == "" {
				hasInitialTransition = true
				continue
			}
			if _, exists := normalisedTransitions[source]; !exists {
				return fmt.Errorf("%w: unknown source status %q", ErrVisionConfigInvalid, source)
			}
		}
	}
	if !hasInitialTransition {
		return fmt.Errorf("%w: no transition from empty status", ErrVisionConfigInvalid)
	}

	c.ValidTypes = normalisedTypes
	c.StatusTransitions = normalisedTransitions
	return nil
}

// toCapabilities returns a defensive, client-safe copy of the config.
func (c *VisionConfig) toCapabilities() *VisionConfigCapabilities {
	transitions := make(map[VisionStatus][]VisionStatus, len(c.StatusTransitions))
	for destination, sources := range c.StatusTransitions {
		transitions[destination] = append([]VisionStatus(nil), sources...)
	}

	return &VisionConfigCapabilities{
		StatusTransitions: transitions,
		ValidTypes:        append([]VisionType(nil), c.ValidTypes...),
		DownvotingEnabled: c.EnableDownvoting,
	}
}
