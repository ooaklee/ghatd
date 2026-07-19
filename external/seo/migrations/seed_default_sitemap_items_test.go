package migrations

import (
	"reflect"
	"testing"
)

func TestResolveDefaultSitemapItemPaths(t *testing.T) {
	got := ResolveDefaultSitemapItemPaths(Paths{
		Add:    []string{"/extra", "extra", "/policy/privacy"},
		Remove: []string{"/blog"},
	})

	want := []string{
		"/",
		"/pricing",
		"/faq",
		"/whats-new",
		"/policy/privacy",
		"/policy/terms",
		"/policy/cookies",
		"/policy/security-and-compliance",
		"/about",
		"/contact",
		"/for-business",
		"/glossary",
		"/extra",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveDefaultSitemapItemPaths() = %#v, want %#v", got, want)
	}
}

func TestResolveDefaultSitemapItemPathsZeroValue(t *testing.T) {
	got := ResolveDefaultSitemapItemPaths(Paths{})

	if !reflect.DeepEqual(got, defaultSitemapItemPaths) {
		t.Fatalf("ResolveDefaultSitemapItemPaths() = %#v, want %#v", got, defaultSitemapItemPaths)
	}
}

func TestResolveDefaultSitemapItemPathsRemoveWinsOverAdd(t *testing.T) {
	remove := append([]string{}, defaultSitemapItemPaths...)
	remove = append(remove, "extra")

	got := ResolveDefaultSitemapItemPaths(Paths{
		Add:    []string{"/extra/", "/kept/"},
		Remove: remove,
	})

	want := []string{"/kept"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveDefaultSitemapItemPaths() = %#v, want %#v", got, want)
	}
}

func TestResolveDefaultSitemapItemPathsCanRemoveAllDefaults(t *testing.T) {
	got := ResolveDefaultSitemapItemPaths(Paths{
		Remove: defaultSitemapItemPaths,
	})

	if len(got) != 0 {
		t.Fatalf("ResolveDefaultSitemapItemPaths() = %#v, want empty", got)
	}
}

func TestNormaliseSitemapSeedPath(t *testing.T) {
	tests := map[string]string{
		"":         "",
		"   ":      "",
		"/":        "/",
		"blog":     "/blog",
		" /blog/ ": "/blog",
		"/faq/":    "/faq",
	}

	for input, want := range tests {
		if got := normaliseSitemapSeedPath(input); got != want {
			t.Fatalf("normaliseSitemapSeedPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDefaultSitemapItemPriority(t *testing.T) {
	tests := map[string]float64{
		"/":                               1.0,
		"/pricing":                        0.9,
		"/contact":                        0.9,
		"/for-business":                   0.9,
		"/blog":                           0.8,
		"/faq":                            0.8,
		"/whats-new":                      0.8,
		"/about":                          0.8,
		"/glossary":                       0.7,
		"/policy/privacy":                 0.4,
		"/policy/security-and-compliance": 0.4,
		"/unexpected":                     0.5,
	}

	for uri, want := range tests {
		if got := DefaultSitemapItemPriority(uri); got != want {
			t.Fatalf("DefaultSitemapItemPriority(%q) = %v, want %v", uri, got, want)
		}
	}
}

func TestInitDefaultSitemapItemsWithPathsReturnsMigrationCallbacks(t *testing.T) {
	up := InitDefaultSitemapItemsUpWithPaths(Paths{
		Add: []string{"/extra"},
	})
	down := InitDefaultSitemapItemsDownWithPaths(Paths{
		Remove: []string{"/blog"},
	})

	if up == nil {
		t.Fatal("InitDefaultSitemapItemsUpWithPaths() returned nil")
	}
	if down == nil {
		t.Fatal("InitDefaultSitemapItemsDownWithPaths() returned nil")
	}
}
