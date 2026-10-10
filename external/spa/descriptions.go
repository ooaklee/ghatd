package spa

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"unicode"
)

// DescriptionInventoryConfig maps build-owned route patterns to static HTML
// shells. Filesystem and public URL locations are independent explicit inputs.
type DescriptionInventoryConfig struct {
	// FS contains the trusted build inventory and HTML shells.
	FS fs.FS
	// InventoryPath is a valid fs.FS path to a JSON array of id/pattern entries.
	InventoryPath string
	// ShellDirectory contains <id>.html files; it must be a valid fs.FS path.
	ShellDirectory string
	// PublicShellPrefix is their clean absolute URL prefix, without a trailing
	// slash except for root. Queries, fragments, escapes and controls are refused.
	PublicShellPrefix string
	// Fallback applies the host's index rewriting and asset bypass policy first.
	// It must return a non-nil request with a URL; only a resulting "/" is eligible.
	Fallback func(*http.Request) *http.Request
	// AllowMissingInventory explicitly permits older builds without the inventory.
	// Other read failures and invalid present inventories always return errors.
	AllowMissingInventory bool
}

var (
	// ErrDescriptionInventoryConfig refuses incomplete or unsafe build locations.
	ErrDescriptionInventoryConfig = errors.New("spa/description-inventory-configuration")
	// ErrDescriptionInventory refuses unreadable or invalid inventory/shell data.
	ErrDescriptionInventory = errors.New("spa/description-inventory")
)

// NewDescriptionPathResolver validates and compiles build rules once. Unknown
// metadata fields are accepted. Ordered patterns may share a shell ID; the first
// matching rule wins. Resolution consults only URL.Path, never query/account data,
// and performs no per-request filesystem I/O. The fallback retains asset routing.
func NewDescriptionPathResolver(cfg DescriptionInventoryConfig) (func(*http.Request) *http.Request, error) {
	if cfg.FS == nil || cfg.Fallback == nil || !fs.ValidPath(cfg.InventoryPath) || !fs.ValidPath(cfg.ShellDirectory) || !validDescriptionPrefix(cfg.PublicShellPrefix) {
		return nil, ErrDescriptionInventoryConfig
	}
	data, err := fs.ReadFile(cfg.FS, cfg.InventoryPath)
	if err != nil {
		if cfg.AllowMissingInventory && errors.Is(err, fs.ErrNotExist) {
			return cfg.Fallback, nil
		}
		return nil, fmt.Errorf("%w: read: %w", ErrDescriptionInventory, err)
	}
	var pages []struct {
		ID      string `json:"id"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(data, &pages); err != nil {
		return nil, fmt.Errorf("%w: JSON: %w", ErrDescriptionInventory, err)
	}
	type rule struct {
		pattern    *regexp.Regexp
		publicPath string
	}
	rules := make([]rule, 0, len(pages))
	validID := regexp.MustCompile(`^[a-z0-9-]+$`)
	for _, page := range pages {
		if !validID.MatchString(page.ID) {
			return nil, fmt.Errorf("%w: invalid shell ID", ErrDescriptionInventory)
		}
		pattern, err := regexp.Compile(page.Pattern)
		if err != nil {
			return nil, fmt.Errorf("%w: pattern: %w", ErrDescriptionInventory, err)
		}
		info, err := fs.Stat(cfg.FS, path.Join(cfg.ShellDirectory, page.ID+".html"))
		if err != nil {
			return nil, fmt.Errorf("%w: shell: %w", ErrDescriptionInventory, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: shell must be a regular file", ErrDescriptionInventory)
		}
		rules = append(rules, rule{pattern: pattern, publicPath: path.Join(cfg.PublicShellPrefix, page.ID+".html")})
	}
	return func(request *http.Request) *http.Request {
		originalPath := request.URL.Path
		request = cfg.Fallback(request)
		if request.URL.Path != "/" {
			return request
		}
		for _, rule := range rules {
			if rule.pattern.MatchString(originalPath) {
				request.URL.Path = rule.publicPath
				request.URL.RawPath = ""
				break
			}
		}
		return request
	}, nil
}

func validDescriptionPrefix(prefix string) bool {
	return strings.HasPrefix(prefix, "/") && prefix == path.Clean(prefix) && !strings.ContainsAny(prefix, "\\?#%") && strings.IndexFunc(prefix, unicode.IsControl) < 0
}
