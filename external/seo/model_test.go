package seo

import "testing"

func TestSitemapItemLastModForXML(t *testing.T) {
	item := SitemapItem{LastMod: "2026-02-21T11:00:00.123456789"}

	got, err := item.LastModForXML()
	if err != nil {
		t.Fatalf("LastModForXML() error = %v", err)
	}

	if got != "2026-02-21T11:00:00+00:00" {
		t.Fatalf("LastModForXML() = %s, want sitemap UTC timestamp", got)
	}
}

func TestChangeFrequencyNormalisesAndValidates(t *testing.T) {
	got := NormaliseChangeFrequency(" Weekly ")
	if got != ChangeFrequencyWeekly {
		t.Fatalf("NormaliseChangeFrequency() = %s, want weekly", got)
	}
	if !got.IsValid() {
		t.Fatal("weekly should be valid")
	}
	if NormaliseChangeFrequency("sometimes").IsValid() {
		t.Fatal("unexpected valid change frequency")
	}
}

func TestNewSitemapItemDefaultsPriorityToPointFive(t *testing.T) {
	item := NewSitemapItem(&CreateSitemapItemRequest{
		URI:             "/policy/terms",
		LastMod:         "2026-02-21T11:00:00",
		ChangeFrequency: ChangeFrequencyWeekly,
	})

	if item.Priority != 0.5 {
		t.Fatalf("NewSitemapItem() priority = %v, want 0.5", item.Priority)
	}
}

func TestValidateSitemapItemRejectsInvalidPriority(t *testing.T) {
	item := &SitemapItem{
		URI:             "/blog/example",
		LastMod:         "2026-02-21T11:00:00",
		Priority:        1.2,
		ChangeFrequency: ChangeFrequencyWeekly,
	}

	if err := validateSitemapItem(item); err != ErrSitemapItemInvalidPriority {
		t.Fatalf("validateSitemapItem() error = %v, want ErrSitemapItemInvalidPriority", err)
	}
}
