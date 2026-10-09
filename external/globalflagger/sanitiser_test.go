package globalflagger

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/stretchr/testify/require"
)

const safeSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><path fill="#00f" d="M0 0h20v10H0z"/></svg>`

func TestSVGRejectsActiveContentAndResourceAbuse(t *testing.T) {
	for name, input := range map[string]string{
		"cycle":            `<svg xmlns="http://www.w3.org/2000/svg"><g id="loop"><use href="#loop"/></g></svg>`,
		"trailing element": `<svg xmlns="http://www.w3.org/2000/svg"/><path/>`,
		"script":           `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"foreignObject":    `<svg><foreignObject/></svg>`, "event": `<svg onload="alert(1)"/>`,
		"external": `<svg><use href="https://attacker.example/flag.svg"/></svg>`,
		"relative": `<svg><use href="/api/private"/></svg>`, "data": `<svg><use href="data:image/svg+xml,x"/></svg>`,
		"css":         `<svg><path style="fill:url(https://attacker.example)"/></svg>`,
		"css escapes": `<svg><path style="fill:u\72l(https://attacker.example)"/></svg>`,
		"doctype":     `<!DOCTYPE svg [<!ENTITY x "boom">]><svg>&x;</svg>`,
		"instruction": `<?xml-stylesheet href="https://attacker.example/style"?><svg/>`,
		"nested":      `<svg><svg/></svg>`, "multiple roots": `<svg/><svg/>`,
		"unresolved": `<svg><use href="#missing"/></svg>`, "duplicate id": `<svg><path id="p"/><path id="p"/></svg>`,
		"depth": "<svg>" + strings.Repeat("<g>", 40) + strings.Repeat("</g>", 40) + "</svg>",
		"size":  strings.Repeat("x", MaxSVGBytes+1), "elements": "<svg>" + strings.Repeat("<path/>", 5001) + "</svg>",
		"unexpected namespace": `<svg xmlns="https://attacker.example"/>`, "event namespace": `<svg xmlns:x="https://attacker.example" x:onload="alert(1)"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(input, "<svg") && !strings.Contains(input, "xmlns=") {
				input = strings.Replace(input, "<svg", `<svg xmlns="http://www.w3.org/2000/svg"`, 1)
			}
			_, err := SanitiseSVG([]byte(input))
			require.Error(t, err)
		})
	}
}
func TestSVGInternalReferencesAndSeeds(t *testing.T) {
	raw := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><defs><path id="p" d="M0 0h10v10z"/></defs><use href="#p" fill="#123"/></svg>`
	sanitised, err := SanitiseSVG([]byte(raw))
	require.NoError(t, err)
	require.Contains(t, string(sanitised), `href="#p"`)
	again, err := SanitiseSVG(sanitised)
	require.NoError(t, err)
	require.Equal(t, sanitised, again)
	all, err := fs.Glob(seedAssets, "seedassets/flags/*.svg")
	require.NoError(t, err)
	require.Len(t, all, 271)
	excluded, err := seedAssets.ReadFile("seedassets/flags/sh-ac.svg")
	require.NoError(t, err)
	_, err = SanitiseSVG(excluded)
	require.Error(t, err, "retained upstream artwork has unresolved paint references")
	assets, err := seedArtworks()
	require.NoError(t, err)
	require.Len(t, assets, 270)
	for _, asset := range assets {
		t.Run(asset.code, func(t *testing.T) {
			require.NotEqual(t, "SH-AC", asset.code)
			raw, err := seedAssets.ReadFile(fmt.Sprintf("seedassets/flags/%s.svg", asset.filename))
			require.NoError(t, err)
			sanitised, err := SanitiseSVG(raw)
			require.NoError(t, err)
			require.NotEmpty(t, sanitised)
			_, err = SanitiseSVG(sanitised)
			require.NoError(t, err)
		})
	}
}

// This single lifecycle sequence verifies audit/revision continuity through
// edits, deletion, restoration and reseeding. Later assertions depend on the
// earlier stored history, rather than independent inputs sharing a fixture.
func TestFlagLifecycleAuditAndInsertionOnlySeeds(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	s := NewService(NewMemoryRepository(nil), catalogue.ClockFunc(func() time.Time { return now }))
	disabled := false
	flag, err := s.Create(ctx, &CreateFlagRequest{Code: "CUSTOM", Name: "Custom flag", SVG: safeSVG, ActorID: "admin-one", Enabled: &disabled})
	require.NoError(t, err)
	require.False(t, flag.Enabled)
	require.Equal(t, "admin-one", flag.CreatedBy)
	require.Equal(t, "admin-one", flag.UpdatedBy)
	require.Equal(t, now, *flag.UpdatedAt)
	now = now.Add(time.Minute)
	updated, err := s.Update(ctx, &UpdateFlagRequest{Code: "CUSTOM", Name: "Updated", SVG: safeSVG, Enabled: true, ExpectedRevision: 1, ActorID: "admin-two"})
	require.NoError(t, err)
	require.Equal(t, flag.CreatedAt, updated.CreatedAt)
	require.Equal(t, "admin-two", updated.UpdatedBy)
	_, err = s.Update(ctx, &UpdateFlagRequest{Code: "CUSTOM", Name: "Old write", ExpectedRevision: 1, ActorID: "admin-one"})
	require.ErrorIs(t, err, catalogue.ErrStaleWrite)
	deleted, err := s.Delete(ctx, &DeleteFlagRequest{Code: "CUSTOM", ExpectedRevision: 2, ActorID: "admin-three"})
	require.NoError(t, err)
	require.Equal(t, "admin-three", deleted.DeletedBy)
	require.Equal(t, "admin-three", deleted.UpdatedBy)
	require.NotNil(t, deleted.DeletedAt)
	public, err := s.ListPublic(ctx, catalogue.ListQuery{IncludeDeleted: true, IncludeHidden: true, IncludeDisabled: true})
	require.NoError(t, err)
	require.Empty(t, public.Flags)
	restored, err := s.Restore(ctx, &RestoreFlagRequest{Code: "CUSTOM", ExpectedRevision: 3, ActorID: "admin-one"})
	require.NoError(t, err)
	require.False(t, restored.Enabled)
	require.Nil(t, restored.DeletedAt)
	require.Empty(t, restored.DeletedBy)
	_, err = s.Seed(ctx)
	require.NoError(t, err)
	gb, err := s.Get(ctx, "GB")
	require.NoError(t, err)
	require.NotEmpty(t, gb.SVG)
	_, err = s.Update(ctx, &UpdateFlagRequest{Code: "GB", Name: "Administrator label", Hidden: true, Enabled: false, ExpectedRevision: gb.Revision, ActorID: "admin"})
	require.NoError(t, err)
	_, err = s.Delete(ctx, &DeleteFlagRequest{Code: "US", ExpectedRevision: 1, ActorID: "admin"})
	require.NoError(t, err)
	seeded, err := s.Seed(ctx)
	require.NoError(t, err)
	require.Zero(t, seeded.Seeded)
	require.Equal(t, 270, seeded.Skipped)
	gb, err = s.Get(ctx, "GB")
	require.NoError(t, err)
	require.Equal(t, "Administrator label", gb.Name)
	require.True(t, gb.Hidden)
	require.False(t, gb.Enabled)
	us, err := s.Get(ctx, "US")
	require.NoError(t, err)
	require.NotNil(t, us.DeletedAt)
	_, err = s.Get(ctx, "MISSING")
	require.True(t, errors.Is(err, catalogue.ErrNotFound))
}
