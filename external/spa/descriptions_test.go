package spa

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func descriptionConfig(data string) DescriptionInventoryConfig {
	return DescriptionInventoryConfig{FS: fstest.MapFS{"build/inventory.json": {Data: []byte(data)}, "build/shells/about.html": {Data: []byte("About")}, "build/shells/docs.html": {Data: []byte("Docs")}}, InventoryPath: "build/inventory.json", ShellDirectory: "build/shells", PublicShellPrefix: "/pages", Fallback: NewHandleUpdatePathToIndex(BypassWithFileExtension("html"))}
}

func TestDescriptionInventoryValidation(t *testing.T) {
	for _, tc := range []struct {
		name, data, prefix, inventory, shells                           string
		nilFS, nilFallback, absent, allowMissing, directory, deniedRead bool
		want                                                            error
	}{
		{name: "valid_metadata_tolerated", data: `[{"id":"about","pattern":"^/about$","description":"build copy","example":"/about"}]`},
		{name: "ordered_patterns_can_share_shell", data: `[{"id":"about","pattern":"^/about$"},{"id":"about","pattern":"^/team$"}]`},
		{name: "empty_inventory", data: `[]`},
		{name: "malformed_json", data: `broken`, want: ErrDescriptionInventory},
		{name: "wrong_top_level", data: `{}`, want: ErrDescriptionInventory},
		{name: "trailing_json", data: `[] []`, want: ErrDescriptionInventory},
		{name: "unsafe_id", data: `[{"id":"../outside","pattern":".*"}]`, want: ErrDescriptionInventory},
		{name: "empty_id", data: `[{"id":"","pattern":".*"}]`, want: ErrDescriptionInventory},
		{name: "invalid_pattern", data: `[{"id":"about","pattern":"["}]`, want: ErrDescriptionInventory},
		{name: "missing_shell", data: `[{"id":"missing","pattern":".*"}]`, want: ErrDescriptionInventory},
		{name: "directory_is_not_shell", directory: true, want: ErrDescriptionInventory},
		{name: "absent_inventory_required", absent: true, want: ErrDescriptionInventory},
		{name: "absent_inventory_opt_in", absent: true, allowMissing: true},
		{name: "allow_missing_does_not_hide_invalid_present_inventory", data: `broken`, allowMissing: true, want: ErrDescriptionInventory},
		{name: "allow_missing_does_not_hide_read_failure", deniedRead: true, allowMissing: true, want: ErrDescriptionInventory},
		{name: "nil_filesystem", nilFS: true, want: ErrDescriptionInventoryConfig},
		{name: "nil_fallback", nilFallback: true, want: ErrDescriptionInventoryConfig},
		{name: "relative_public_prefix", prefix: "pages", want: ErrDescriptionInventoryConfig},
		{name: "public_traversal", prefix: "/pages/../outside", want: ErrDescriptionInventoryConfig},
		{name: "public_query", prefix: "/pages?token=fixture", want: ErrDescriptionInventoryConfig},
		{name: "encoded_public_prefix", prefix: "/%2e%2e", want: ErrDescriptionInventoryConfig},
		{name: "inventory_traversal", inventory: "../inventory.json", want: ErrDescriptionInventoryConfig},
		{name: "shell_traversal", shells: "../shells", want: ErrDescriptionInventoryConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.data
			if data == "" {
				data = `[{"id":"about","pattern":"^/about$"}]`
			}
			cfg := descriptionConfig(data)
			if tc.prefix != "" {
				cfg.PublicShellPrefix = tc.prefix
			}
			if tc.inventory != "" {
				cfg.InventoryPath = tc.inventory
			}
			if tc.shells != "" {
				cfg.ShellDirectory = tc.shells
			}
			cfg.AllowMissingInventory = tc.allowMissing
			if tc.absent {
				delete(cfg.FS.(fstest.MapFS), "build/inventory.json")
			}
			if tc.directory {
				cfg.FS.(fstest.MapFS)["build/shells/about.html"].Mode = fs.ModeDir
			}
			if tc.nilFS {
				cfg.FS = nil
			}
			if tc.deniedRead {
				cfg.FS = deniedDescriptionFS{}
			}
			if tc.nilFallback {
				cfg.Fallback = nil
			}
			resolver, err := NewDescriptionPathResolver(cfg)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, resolver)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resolver)
		})
	}
}

func TestDescriptionPathResolution(t *testing.T) {
	for _, tc := range []struct{ name, target, want, prefix string }{
		{"pathname_match", "/about", "/pages/about.html", ""},
		{"query_not_consulted", "/about?private-token=ignored", "/pages/about.html", ""},
		{"query_cannot_select_shell", "/unknown?path=/about", "/", ""},
		{"private_segment_replaced", "/docs/private-token", "/pages/docs.html", ""},
		{"encoded_path_clears_stale_raw_path", "/docs/private%2Dtoken", "/pages/docs.html", ""},
		{"asset_bypass", "/assets/app.js", "/assets/app.js", ""},
		{"html_bypass", "/pages/about.html", "/pages/about.html", ""},
		{"shared_shell_alternative_pattern", "/team", "/pages/about.html", ""},
		{"overlap_first_match_wins", "/about/specific", "/pages/about.html", ""},
		{"unmatched_index_fallback", "/unknown", "/", ""},
		{"public_prefix_independent_of_filesystem", "/about", "/about.html", "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := descriptionConfig(`[{"id":"about","pattern":"^/about"},{"id":"docs","pattern":"^/about/specific$"},{"id":"about","pattern":"^/team$"},{"id":"docs","pattern":"^/docs/[^/]+$"}]`)
			if tc.prefix != "" {
				cfg.PublicShellPrefix = tc.prefix
			}
			resolve, err := NewDescriptionPathResolver(cfg)
			require.NoError(t, err)
			r := httptest.NewRequest(http.MethodGet, tc.target, nil)
			originalPath := r.URL.Path
			query := r.URL.RawQuery
			r.URL.RawPath = r.URL.EscapedPath()
			actual := resolve(r)
			require.Equal(t, tc.want, actual.URL.Path)
			require.Equal(t, query, actual.URL.RawQuery)
			if actual.URL.Path != originalPath && actual.URL.Path != "/" {
				require.Empty(t, actual.URL.RawPath)
				require.Equal(t, tc.want, actual.URL.EscapedPath())
			}
		})
	}
}

type deniedDescriptionFS struct{}

func (deniedDescriptionFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}
