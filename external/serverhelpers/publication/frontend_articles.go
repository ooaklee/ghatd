package publication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/microcosm-cc/bluemonday"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ArticleSource reads through shared CMS public authorization; callers
// supply no ActorID and never enrich the request from browser credentials.
type ArticleSource interface {
	GetArticleItemByUrlFriendlyId(context.Context, *contentmanager.GetArticleItemByUrlFriendlyIdRequest) (*post.Post, error)
	GetArticles(context.Context, *contentmanager.GetArticlesRequest) (*contentmanager.GetArticlesResponse, error)
}

// publicArticlePages owns immutable shell/policy configuration after startup.
type publicArticlePages struct {
	source ArticleSource
	shell  []byte
	now    func() time.Time
	// origin is configured by the host; an incoming Host cannot choose canonicals.
	origin       string
	brand        string
	descriptions map[string]string
	bootstrapID  string
	indexHeading string
	presentation func(string) (string, error)
	sanitize     func(string) string
	pathAllowed  func(string) bool
}

// Only public reader fields are bootstrapped, never CMS editor/author account IDs.
type publicArticleBootstrap struct {
	ID                    string               `json:"id"`
	Slug                  string               `json:"url_friendly_id"`
	Title                 string               `json:"title"`
	Text                  string               `json:"text"`
	TextFormat            post.TextFormat      `json:"text_format"`
	HeaderImage           string               `json:"header_image,omitempty"`
	HeaderImageType       post.HeaderImageType `json:"header_image_type,omitempty"`
	HeaderImageAltText    string               `json:"header_image_alt_text,omitempty"`
	PublishedAs           string               `json:"published_as,omitempty"`
	PublishedAt           string               `json:"published_at"`
	Tags                  []string             `json:"tags,omitempty"`
	PublishedArticlePaths []string             `json:"published_article_paths"`
}

var publicArticleSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var publicArticleLink = regexp.MustCompile(`^/blog/([a-z0-9]+(?:-[a-z0-9]+)*)(?:#[^\s?]*)?$`)
var publicArticleMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))
var publicArticleSanitizer = bluemonday.UGCPolicy().RequireNoFollowOnLinks(false)

// The same initial HTML is returned to readers and crawlers. Vue replaces this
// public document after mounting; privileged CMS previews remain on the API.
// PagesConfig supplies public reader dependencies and host presentation choices.
type PagesConfig struct {
	Files                                    fs.FS
	Prefix                                   string
	Source                                   ArticleSource
	Origin, Brand, BootstrapID, IndexHeading string
	Now                                      func() time.Time
	PathAllowed                              func(string) bool
	// MarkdownPresentation transforms rendered Markdown before sanitization.
	MarkdownPresentation func(string) (string, error)
	// Sanitize overrides the default UGC policy; it must produce safe HTML.
	Sanitize func(string) string
}

// AttachPublicArticlePages registers anonymous public article routes.
func AttachPublicArticlePages(router *mux.Router, config PagesConfig) error {
	if router == nil {
		return errors.New("public article router unavailable")
	}
	pages, err := NewPages(config)
	if err != nil || pages == nil {
		return err
	}
	router.Handle("/blog", pages).Methods(http.MethodGet, http.MethodHead)
	router.PathPrefix("/blog/").Handler(pages).Methods(http.MethodGet, http.MethodHead)
	return nil
}

// NewPages builds an anonymous reader, or returns nil when the build disables it.
func NewPages(config PagesConfig) (http.Handler, error) {
	files, prefix, source, origin, brand := config.Files, config.Prefix, config.Source, config.Origin, config.Brand

	configured, err := url.Parse(origin)
	if err != nil || configured.Host == "" || (configured.Scheme != "http" && configured.Scheme != "https") || configured.User != nil || (configured.Path != "" && configured.Path != "/") || configured.RawQuery != "" || configured.Fragment != "" {
		return nil, errors.New("invalid public article origin")
	}
	origin = strings.TrimSuffix(configured.String(), "/")
	if files == nil {
		return nil, nil // The existing backend-only mode has no public frontend assets.
	}
	allowed, err := readPublicSitemapPolicy(files, path.Join(prefix, "dist"))
	if err != nil {
		return nil, err
	}
	if (allowed != nil && !allowed["/blog"]) || (config.PathAllowed != nil && !config.PathAllowed("/blog")) {
		return nil, nil // The mailing-list build must not expose public CMS content.
	}
	if config.BootstrapID == "" {
		return nil, errors.New("public article bootstrap ID required")
	}
	if source == nil {
		return nil, errors.New("public article source unavailable")
	}
	shell, err := fs.ReadFile(files, path.Join(prefix, "dist/index.html"))
	if err != nil {
		return nil, err
	}
	descriptions, err := readPublicArticleDescriptions(files, prefix)
	if err != nil {
		return nil, err
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	pages := &publicArticlePages{source: source, shell: shell, now: now, origin: origin, brand: brand, descriptions: descriptions, bootstrapID: config.BootstrapID, indexHeading: config.IndexHeading, presentation: config.MarkdownPresentation, sanitize: config.Sanitize, pathAllowed: config.PathAllowed}
	return pages, nil
}

func (p *publicArticlePages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	pathname := strings.TrimSuffix(r.URL.Path, "/")
	if p.pathAllowed != nil && !p.pathAllowed(pathname) {
		p.unavailable(w, r, http.StatusNotFound)
		return
	}
	if pathname == "/blog" {
		if r.URL.Path != pathname {
			http.Redirect(w, r, pathname+articleQuery(r), http.StatusMovedPermanently)
			return
		}
		p.serveIndex(ctx, w, r)
		return
	}
	slug := strings.TrimPrefix(pathname, "/blog/")
	if !publicArticleSlug.MatchString(slug) {
		p.unavailable(w, r, http.StatusNotFound)
		return
	}
	lookup := slug
	if !strings.HasPrefix(lookup, "article-") {
		lookup = "article-" + lookup
	}
	// No ActorID is supplied, even for a signed-in administrator's request.
	article, err := p.source.GetArticleItemByUrlFriendlyId(ctx, &contentmanager.GetArticleItemByUrlFriendlyIdRequest{UrlFriendlyId: lookup})
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, post.ErrResourceNotFound) || errors.Is(err, contentmanager.ErrUnauthorisedCMUser) {
			status = http.StatusNotFound
		}
		p.unavailable(w, r, status)
		return
	}
	if !contentmanager.IsPublicPostAt(article, p.now()) {
		p.unavailable(w, r, http.StatusNotFound)
		return
	}
	canonicalPath := publicArticlePath(article)
	if canonicalPath == "" {
		p.unavailable(w, r, http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path != canonicalPath {
		http.Redirect(w, r, canonicalPath+articleQuery(r), http.StatusMovedPermanently)
		return
	}
	body := article.Text
	if article.TextFormat == post.TextFormatMarkdown {
		var rendered bytes.Buffer
		if err := publicArticleMarkdown.Convert([]byte(body), &rendered); err != nil {
			p.unavailable(w, r, http.StatusServiceUnavailable)
			return
		}
		body = rendered.String()
		if p.presentation != nil {
			body, err = p.presentation(body)
			if err != nil {
				p.unavailable(w, r, http.StatusServiceUnavailable)
				return
			}
		}
	}
	if p.sanitize != nil {
		body = p.sanitize(body)
	} else {
		body = publicArticleSanitizer.Sanitize(body)
	}
	// Match the client reader: future/deleted related articles are plain prose.
	body, publishedPaths := p.publishedLinks(ctx, body)
	seedText := body // Bootstrap only sanitized rendered HTML; never raw Markdown HTML.
	seed, _ := json.Marshal(publicArticleBootstrap{
		ID: article.Id, Slug: article.UrlFriendlyId, Title: article.Title, Text: seedText, TextFormat: post.TextFormatHtml,
		HeaderImage: article.HeaderImage, HeaderImageType: article.HeaderImageType, HeaderImageAltText: article.HeaderImageAltText,
		PublishedAs: article.PublishedAs, PublishedAt: publicArticleTimestamp(article.PublishedAt), Tags: article.Tags, PublishedArticlePaths: publishedPaths,
	})
	description := articleSummary(body)
	if description == "" {
		description = p.descriptions["blog-specific"]
	}
	author := article.PublishedAs
	if author == "" {
		author = p.brand + " team"
	}
	content := `<main class="blog-page blog-reader landing-container"><nav><a href="/">` + html.EscapeString(p.brand) + `</a> · <a href="/blog">All stories</a></nav><article><header><h1>` + html.EscapeString(article.Title) + `</h1><p>` + html.EscapeString(author) + `</p></header><div class="public-content">` + body + `</div></article></main>`
	p.writePage(w, r, http.StatusOK, article.Title+" - Blog", description, canonicalPath, "article", content, seed)
}

func articleQuery(r *http.Request) string {
	if r.URL.RawQuery != "" {
		return "?" + r.URL.RawQuery
	}
	return ""
}

func publicArticlePath(article *post.Post) string {
	if article == nil || !strings.HasPrefix(article.UrlFriendlyId, "article-") {
		return ""
	}
	slug := strings.TrimPrefix(article.UrlFriendlyId, "article-")
	if !publicArticleSlug.MatchString(slug) {
		return ""
	}
	return "/blog/" + slug
}

func (p *publicArticlePages) serveIndex(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var content strings.Builder
	heading := p.indexHeading
	if heading == "" {
		heading = "Blog"
	}
	content.WriteString(`<main class="blog-page landing-container"><nav><a href="/">` + html.EscapeString(p.brand) + `</a></nav><h1>` + html.EscapeString(heading) + `</h1><ul>`)
	for page := 1; page <= 100; page++ {
		result, err := p.source.GetArticles(ctx, &contentmanager.GetArticlesRequest{GetArticlesRequest: &post.GetArticlesRequest{
			GetPostsRequest: &post.GetPostsRequest{Page: page, PerPage: 100, Meta: true},
		}})
		if err != nil || result == nil || result.GetArticlesResponse == nil || result.GetPostsResponse == nil {
			p.unavailable(w, r, http.StatusServiceUnavailable)
			return
		}
		for i := range result.Posts {
			article := &result.Posts[i]
			uri := publicArticlePath(article)
			if uri != "" && (p.pathAllowed == nil || p.pathAllowed(uri)) && contentmanager.IsPublicPostAt(article, p.now()) {
				fmt.Fprintf(&content, `<li><a href="%s">%s</a></li>`, uri, html.EscapeString(article.Title))
			}
		}
		if page >= result.TotalPages {
			content.WriteString(`</ul></main>`)
			p.writePage(w, r, http.StatusOK, "Blog - "+p.brand, p.descriptions["blog"], "/blog", "website", content.String(), nil)
			return
		}
	}
	p.unavailable(w, r, http.StatusServiceUnavailable)
}

func (p *publicArticlePages) unavailable(w http.ResponseWriter, r *http.Request, status int) {
	w.Header().Del("Link")
	if status == http.StatusNotFound {
		w.Header().Set("X-Robots-Tag", "noindex")
	}
	title, message := "Story unavailable - "+p.brand, "This story couldn’t be loaded. Please try again in a moment."
	if status == http.StatusNotFound {
		title, message = "Story not found - "+p.brand, "We couldn’t find that story. It may have moved or is no longer available."
	}
	p.writePage(w, r, status, title, message, "", "website", `<main><h1>`+html.EscapeString(title)+`</h1><p>`+html.EscapeString(message)+`</p><a href="/blog">Back to the blog</a></main>`, nil)
}

// Only public rendered HTML enters the trusted body slot; metadata stays text.
func (p *publicArticlePages) writePage(w http.ResponseWriter, r *http.Request, status int, title, description, canonical, kind, body string, seed []byte) {
	output, err := articlePageHTML(p.shell, p.origin, title, description, canonical, kind, body, seed, p.bootstrapID)
	if err != nil {
		http.Error(w, "Page temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(output)
	}
}

func articlePageHTML(shell []byte, origin, title, description, canonical, kind, body string, seed []byte, bootstrapID string) ([]byte, error) {
	doc, err := xhtml.Parse(bytes.NewReader(shell))
	if err != nil {
		return nil, err
	}
	head := articleNode(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "head" })
	app := articleNode(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && articleAttr(n, "id") == "app" })
	if head == nil || app == nil {
		return nil, errors.New("article shell is missing head or app")
	}
	for child := head.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == xhtml.ElementNode && (child.Data == "title" || child.Data == "link" && articleAttr(child, "rel") == "canonical") {
			head.RemoveChild(child)
		}
		child = next
	}
	if canonical != "" {
		head.AppendChild(&xhtml.Node{Type: xhtml.ElementNode, Data: "link", DataAtom: atom.Link, Attr: []xhtml.Attribute{{Key: "rel", Val: "canonical"}, {Key: "href", Val: origin + canonical}}})
	}
	titleNode := &xhtml.Node{Type: xhtml.ElementNode, Data: "title", DataAtom: atom.Title}
	titleNode.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: title})
	head.AppendChild(titleNode)
	if len(seed) > 0 {
		bootstrap := &xhtml.Node{Type: xhtml.ElementNode, Data: "script", DataAtom: atom.Script, Attr: []xhtml.Attribute{{Key: "id", Val: bootstrapID}, {Key: "type", Val: "application/json"}}}
		// encoding/json escapes '<', '>' and '&', including closing script tags.
		bootstrap.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: string(seed)})
		head.AppendChild(bootstrap)
	}
	values := map[string]string{"name:title": title, "name:description": description, "itemprop:name": title, "itemprop:description": description, "property:og:title": title, "property:og:description": description, "property:og:type": kind, "name:twitter:title": title, "name:twitter:description": description}
	if canonical != "" {
		values["property:og:url"] = origin + canonical
	} else {
		values["property:og:url"] = ""
	}
	for n := head.FirstChild; n != nil; n = n.NextSibling {
		if n.Type != xhtml.ElementNode || n.Data != "meta" {
			continue
		}
		for attribute, name := range map[string]string{"name": articleAttr(n, "name"), "property": articleAttr(n, "property"), "itemprop": articleAttr(n, "itemprop")} {
			if value, ok := values[attribute+":"+name]; ok {
				for i := range n.Attr {
					if n.Attr[i].Key == "content" {
						n.Attr[i].Val = value
					}
				}
			}
		}
	}
	for app.FirstChild != nil {
		app.RemoveChild(app.FirstChild)
	}
	fragment, err := xhtml.ParseFragment(strings.NewReader(body), app)
	if err != nil {
		return nil, err
	}
	for _, n := range fragment {
		app.AppendChild(n)
	}
	var output bytes.Buffer
	if err := xhtml.Render(&output, doc); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func articleNode(n *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	if match(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := articleNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func articleAttr(n *xhtml.Node, key string) string {
	for _, attribute := range n.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func articleSummary(body string) string {
	doc, _ := xhtml.Parse(strings.NewReader(body))
	paragraph := articleNode(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "p" })
	if paragraph == nil {
		paragraph = doc
	}
	var text strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			text.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "p", "div", "li", "br", "h1", "h2", "h3", "h4", "blockquote":
				text.WriteByte(' ')
			}
		}
	}
	walk(paragraph)
	if strings.TrimSpace(text.String()) == "" && paragraph != doc {
		text.Reset()
		walk(doc)
	}
	runes := []rune(strings.Join(strings.Fields(text.String()), " "))
	if len(runes) > 165 {
		return strings.TrimSpace(string(runes[:164])) + "…"
	}
	return string(runes)
}

func (p *publicArticlePages) publishedLinks(ctx context.Context, body string) (string, []string) {
	doc, err := xhtml.Parse(strings.NewReader(body))
	if err != nil {
		return body, []string{}
	}
	var anchors []*xhtml.Node
	ids := map[string]bool{}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "a" {
			if match := publicArticleLink.FindStringSubmatch(articleAttr(n, "href")); match != nil {
				anchors = append(anchors, n)
				if len(ids) < 10000 {
					ids["article-"+match[1]] = true
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if len(anchors) == 0 {
		return body, []string{}
	}
	var requested []string
	for id := range ids {
		requested = append(requested, id)
	}
	published := map[string]bool{}
	sort.Strings(requested)
	for start := 0; start < len(requested); start += 100 {
		end := min(start+100, len(requested))
		result, err := p.source.GetArticles(ctx, &contentmanager.GetArticlesRequest{GetArticlesRequest: &post.GetArticlesRequest{GetPostsRequest: &post.GetPostsRequest{PerPage: 100, UrlFriendlyIds: strings.Join(requested[start:end], ",")}}})
		if err == nil && result != nil && result.GetArticlesResponse != nil && result.GetPostsResponse != nil {
			for i := range result.Posts {
				if contentmanager.IsPublicPostAt(&result.Posts[i], p.now()) {
					pathname := publicArticlePath(&result.Posts[i])
					if p.pathAllowed == nil || p.pathAllowed(pathname) {
						published[pathname] = true
					}
				}
			}
		}
	}
	for _, anchor := range anchors {
		match := publicArticleLink.FindStringSubmatch(articleAttr(anchor, "href"))
		if !published["/blog/"+match[1]] {
			anchor.Data, anchor.DataAtom, anchor.Attr = "span", atom.Span, nil
		}
	}
	bodyNode := articleNode(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "body" })
	var output bytes.Buffer
	for n := bodyNode.FirstChild; n != nil; n = n.NextSibling {
		_ = xhtml.Render(&output, n)
	}
	paths := make([]string, 0, len(published))
	for path := range published {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return output.String(), paths
}

// Public bootstrap timestamps carry an explicit UTC zone for browser portability.
func publicArticleTimestamp(value string) string {
	when, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		when, err = time.Parse("2006-01-02T15:04:05", value)
	}
	if err != nil {
		return ""
	}
	return when.UTC().Format(time.RFC3339Nano)
}

// Enabled article routes require the matching compiled descriptions. Empty
// article bodies use this inventory instead of inventing generic fallback copy.
func readPublicArticleDescriptions(files fs.FS, prefix string) (map[string]string, error) {
	data, err := fs.ReadFile(files, path.Join(prefix, "dist/page-descriptions.json"))
	if err != nil {
		return nil, err
	}
	var pages []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &pages); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, page := range pages {
		if page.ID == "blog" || page.ID == "blog-specific" {
			if strings.TrimSpace(page.Description) == "" || result[page.ID] != "" {
				return nil, errors.New("invalid public article description inventory")
			}
			result[page.ID] = page.Description
		}
	}
	if result["blog"] == "" || result["blog-specific"] == "" {
		return nil, errors.New("public article descriptions required")
	}
	return result, nil
}
