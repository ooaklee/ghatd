package adminaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/accesspolicymanager"
)

// basePath is selected only from a closed trusted configuration enum.
func (b *Bridge) basePath() string {
	if b.config.ReviewKind == Capabilities {
		return CapabilityBasePath
	}
	return BasePath
}

// codeHash preserves deployed token proofs byte-for-byte and separates every
// capability proof from both those proofs and the original token reader.
func (b *Bridge) codeHash(id, code string) string {
	if b.config.ReviewKind == Capabilities {
		return digest("admin-capability-grant-v1\x00" + id + "\x00" + code)
	}
	return codeHash(id, code)
}

// validReview checks purpose and payload presence before proof or dispatch.
func (b *Bridge) validReview(r review) bool {
	if b.config.ReviewKind == Capabilities {
		return r.Purpose == string(Capabilities) && r.Capabilities != nil && accesspolicy.ValidateCapabilities(*r.Capabilities) == nil && r.Limits == (accesspolicy.TokenLimits{})
	}
	return r.Purpose == "" && r.Capabilities == nil
}

// sameCapabilities compares canonical sets and Mongo-precision UTC expiry.
// Invalid duplicates stay invalid rather than disappearing during sorting.
func sameCapabilities(a, b accesspolicy.Capabilities) bool {
	a, ae := accesspolicy.CanonicalCapabilities(a)
	b, be := accesspolicy.CanonicalCapabilities(b)
	return ae == nil && be == nil && a.Enabled == b.Enabled && a.ExpiresAt.Equal(b.ExpiresAt) && slices.Equal(a.Scopes, b.Scopes) && slices.Equal(a.Permissions, b.Permissions)
}

// ReviewCapabilities requires a bound page and delegates current owner reads.
// It creates no approval or email challenge and never calls an apply port.
func (c *command) ReviewCapabilities(ctx context.Context, target string) (*accesspolicy.Grant, error) {
	b := c.bridge
	if b.config.ReviewKind != Capabilities {
		return nil, ErrReview
	}
	binding, err := b.identity(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = b.config.Store.run(ctx, one(c.request, ContextHeader), binding, "check", randomID(), "", "", 0); err != nil {
		return nil, err
	}
	return b.config.Manager.(CapabilityManager).ReviewCapabilities(ctx, target)
}

// PreviewCapabilities freezes a canonical complete replacement and current
// revision. A no-op invalidates any earlier approval but issues no new review.
func (c *command) PreviewCapabilities(ctx context.Context, target string, patch accesspolicy.Capabilities) (accesspolicy.CapabilityPreview, error) {
	b := c.bridge
	if b.config.ReviewKind != Capabilities {
		return accesspolicy.CapabilityPreview{}, ErrReview
	}
	binding, err := b.identity(ctx)
	if err != nil {
		return accesspolicy.CapabilityPreview{}, err
	}
	if _, err = b.config.Store.run(ctx, one(c.request, ContextHeader), binding, "check", randomID(), "", "", 0); err != nil {
		return accesspolicy.CapabilityPreview{}, err
	}
	patch, err = accesspolicy.CanonicalCapabilities(patch)
	if err != nil {
		return accesspolicy.CapabilityPreview{}, err
	}
	preview, err := b.config.Manager.(CapabilityManager).PreviewCapabilities(ctx, target, patch)
	if err != nil {
		return accesspolicy.CapabilityPreview{}, err
	}
	r := review{ID: randomID(), Target: target, Purpose: string(Capabilities), Capabilities: &patch}
	if preview.Before != nil {
		r.Revision = preview.Before.Revision
		before, e := accesspolicy.CanonicalCapabilities(accesspolicy.Capabilities{Enabled: preview.Before.Enabled, ExpiresAt: preview.Before.ExpiresAt, Scopes: preview.Before.Scopes, Permissions: preview.Before.Permissions})
		if e != nil {
			return accesspolicy.CapabilityPreview{}, e
		}
		r.BeforeCapabilities = &before
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return accesspolicy.CapabilityPreview{}, ErrUnavailable
	}
	r, err = b.config.Store.run(ctx, one(c.request, ContextHeader), binding, "preview", r.ID, string(payload), "", b.config.Window)
	if err != nil {
		return accesspolicy.CapabilityPreview{}, err
	}
	if !preview.Changed {
		_, err = b.config.Store.run(ctx, one(c.request, ContextHeader), binding, "cancel", r.ID, "", "", 0)
		return preview, err
	}
	c.writer.Header().Set(ReviewHeader, r.ID)
	c.writer.Header().Set("X-Admin-Expires", strconv.FormatInt(r.Expires, 10))
	c.writer.Header().Set("X-Admin-Remaining-Ms", strconv.FormatInt(r.Expires-r.ObservedAt, 10))
	return preview, nil
}

// ApplyCapabilities compares every immutable field before one-shot consumption.
// Subsequent owner CAS failures/uncertainty never restore approval.
func (c *command) ApplyCapabilities(ctx context.Context, target string, revision int64, patch accesspolicy.Capabilities) (accesspolicy.Grant, error) {
	b := c.bridge
	if b.config.ReviewKind != Capabilities {
		return accesspolicy.Grant{}, ErrReview
	}
	patch, err := accesspolicy.CanonicalCapabilities(patch)
	if err != nil {
		return accesspolicy.Grant{}, ErrReview
	}
	binding, err := b.identity(ctx)
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	id, rid := one(c.request, ContextHeader), one(c.request, ReviewHeader)
	r, err := b.config.Store.run(ctx, id, binding, "read", rid, "", "", 0)
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	if !b.validReview(r) || r.Target != target || r.Revision != revision || !sameCapabilities(*r.Capabilities, patch) {
		return accesspolicy.Grant{}, ErrReview
	}
	if _, err = b.config.Store.run(ctx, id, binding, "consume", rid, "", "", 0); err != nil {
		return accesspolicy.Grant{}, err
	}
	b.record(ctx, "apply_dispatched", r)
	grant, err := b.config.Manager.(CapabilityManager).ApplyCapabilities(ctx, target, revision, patch)
	if err != nil {
		b.record(ctx, "apply_unconfirmed", r)
	} else {
		b.record(ctx, "apply_confirmed", r)
	}
	return grant, err
}

// capabilityPolicy delegates strict decoding, ETags and reply manifests to the
// same owner handler as the unchanged bearer API.
func (b *Bridge) capabilityPolicy(w http.ResponseWriter, r *http.Request, operation string) {
	h, err := accesspolicymanager.NewHandler(&command{b, r, w}, ErrorMap)
	if err != nil {
		b.fail(w, err)
		return
	}
	switch operation {
	case "review":
		if !emptyBody(r) {
			b.fail(w, accesspolicymanager.ErrInvalidRequest)
			return
		}
		h.ReviewCapabilities(w, r)
	case "preview":
		h.PreviewCapabilities(w, r)
	case "apply":
		h.ApplyCapabilities(w, r)
	}
}
func (b *Bridge) reviewCapabilities(w http.ResponseWriter, r *http.Request) {
	b.capabilityPolicy(w, r, "review")
}
func (b *Bridge) previewCapabilities(w http.ResponseWriter, r *http.Request) {
	b.capabilityPolicy(w, r, "preview")
}
func (b *Bridge) applyCapabilities(w http.ResponseWriter, r *http.Request) {
	b.capabilityPolicy(w, r, "apply")
}

// capabilityChanges names exact additions/removals without treating order as
// authority. An absent grant has empty prior sets; all values are HTML escaped.
func capabilityChanges(r review) string {
	before := accesspolicy.Capabilities{}
	if r.BeforeCapabilities != nil {
		before = *r.BeforeCapabilities
	}
	after := *r.Capabilities
	addedScopes, removedScopes, addedActions, removedActions := []string{}, []string{}, []string{}, []string{}
	for _, v := range after.Scopes {
		if !slices.Contains(before.Scopes, v) {
			addedScopes = append(addedScopes, v)
		}
	}
	for _, v := range before.Scopes {
		if !slices.Contains(after.Scopes, v) {
			removedScopes = append(removedScopes, v)
		}
	}
	for _, v := range after.Permissions {
		if !slices.Contains(before.Permissions, v) {
			addedActions = append(addedActions, v)
		}
	}
	for _, v := range before.Permissions {
		if !slices.Contains(after.Permissions, v) {
			removedActions = append(removedActions, v)
		}
	}
	raw, _ := json.Marshal(map[string][]string{"added_scopes": addedScopes, "removed_scopes": removedScopes, "added_permissions": addedActions, "removed_permissions": removedActions})
	return string(raw)
}
