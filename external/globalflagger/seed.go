package globalflagger

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/ooaklee/ghatd/external/catalogue"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// seedAssets embeds the pinned flag-icons artwork (MIT). Files stay
// lowercase on disk; the exposed flag Code/ID is the UPPERCASE form.
//
//go:embed seedassets/flags/*.svg seedassets/LICENSE seedassets/source.json
var seedAssets embed.FS

// SeedResult reports completed insertion-only seeding counts. Seed returns a nil
// result on error; callers inspect the error rather than a partial failure count.
type SeedResult struct {
	Seeded  int `json:"seeded"`
	Skipped int `json:"skipped"`
}

// SeedLoads embed the raw seed artwork for the sanitiser tests so the
// pinned real flag-icons examples are validated through the normal path —
// no bypass for seeds.
type seedArtwork struct {
	filename string // lowercase asset filename (e.g. "gb.svg")
	code     string // UPPERCASE stable identifier (e.g. "GB")
}

// canonicalSeed maps an asset filename to its UPPERCASE exposed code.
// Filename rules: region codes/subdivisions like "gb.svg", "eu.svg",
// "gb-sct.svg" map by uppercasing; organisation flags (arab, asean, cefta,
// eac) keep their organisation identifiers as codes.
var canonicalSeedAliases = map[string]string{
	"arab":  "ARAB",
	"asean": "ASEAN",
	"cefta": "CEFTA",
	"eac":   "EAC",
}

// seedArtworks enumerates the embedded assets in deterministic order.
func seedArtworks() ([]seedArtwork, error) {
	entries, err := fs.Glob(seedAssets, "seedassets/flags/*.svg")
	if err != nil {
		return nil, err
	}
	artworks := make([]seedArtwork, 0, len(entries))
	for _, path := range entries {
		// This pinned upstream asset has 62 dangling gradient references.
		// Keep it as source evidence, but do not publish broken artwork.
		if strings.HasSuffix(path, "/sh-ac.svg") {
			continue
		}
		filename := strings.TrimSuffix(strings.TrimPrefix(path, "seedassets/flags/"), ".svg")
		code := strings.ToUpper(filename)
		if alias, ok := canonicalSeedAliases[filename]; ok {
			code = alias
		}
		artworks = append(artworks, seedArtwork{filename: filename, code: code})
	}
	return artworks, nil
}

// seedName labels regions in British English; organisation and subdivision
// identities have explicit labels instead of pretending to be country codes.
func seedName(code string) string {
	special := map[string]string{"EU": "European Union", "UN": "United Nations", "ARAB": "Arab League", "ASEAN": "Association of Southeast Asian Nations", "CEFTA": "Central European Free Trade Agreement", "EAC": "East African Community", "GB-ENG": "England", "GB-SCT": "Scotland", "GB-WLS": "Wales", "SH-AC": "Ascension Island", "SH-HL": "Saint Helena", "SH-TA": "Tristan da Cunha"}
	if name, ok := special[code]; ok {
		return name
	}
	if region, err := language.ParseRegion(code); err == nil {
		return display.BritishEnglish.Regions().Name(region)
	}
	return code
}

// Seed idempotently loads the embedded flag catalogue through the
// Repository port. Every seed SVG passes through SanitiseSVG — seeds are
// validated exactly like admin uploads, never bypassed. Existing records
// (including admin edited, disabled, hidden or deleted ones) are never
// overwritten: InsertIfAbsent semantics. Re-running is always safe.
//
// Attribution uses catalogue.SystemSeedActor. A sanitiser failure for one
// asset fails the whole call: shipping a bypassed or unsanitised seed is
// never acceptable.
func (s *Service) Seed(ctx context.Context) (*SeedResult, error) {
	artworks, err := seedArtworks()
	if err != nil {
		return nil, fmt.Errorf("%w: enumerating seed assets: %v", catalogue.ErrUnavailable, err)
	}
	result := &SeedResult{}
	now := s.clock.Now().UTC()
	for _, artwork := range artworks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := seedAssets.ReadFile(fmt.Sprintf("seedassets/flags/%s.svg", artwork.filename))
		if err != nil {
			return nil, fmt.Errorf("%w: reading seed asset %s: %v", catalogue.ErrUnavailable, artwork.filename, err)
		}
		sanitised, err := SanitiseSVG(raw)
		if err != nil {
			return nil, fmt.Errorf("seed asset %s failed sanitisation: %w", artwork.filename, err)
		}
		record := &Record{
			Code:    artwork.code,
			Name:    seedName(artwork.code),
			Enabled: true, // flags are enabled by default
			Hidden:  false,
			SVG:     string(sanitised),
			Audit: catalogue.Audit{
				Revision:  1,
				CreatedAt: now,
				CreatedBy: catalogue.SystemSeedActor,
				UpdatedAt: &now,
				UpdatedBy: catalogue.SystemSeedActor,
			},
		}
		_, inserted, err := s.repo().InsertIfAbsent(ctx, record)
		if err != nil {
			return nil, err
		}
		if inserted {
			result.Seeded++
		} else {
			result.Skipped++
		}
	}
	return result, nil
}
