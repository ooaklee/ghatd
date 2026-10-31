package publication

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// articleMarkupPolicy retains only the reader's generated heading/CTA features
// alongside ordinary UGC markup. Arbitrary styling and event handlers stay out.
// MarkdownConfig describes the host reader's standalone CTA presentation.
type MarkdownConfig struct {
	CTATitle, ParagraphClass, LinkClass, Arrow string
	AllowTarget                                func(string) bool
}

// MarkdownSanitizer permits only the configured presentation classes and safe UGC.
func MarkdownSanitizer(c MarkdownConfig) func(string) string {
	policy := bluemonday.UGCPolicy().RequireNoFollowOnLinks(false)
	if c.ParagraphClass != "" {
		policy.AllowAttrs("class").Matching(regexp.MustCompile("^" + regexp.QuoteMeta(c.ParagraphClass) + "$")).OnElements("p")
	}
	if c.LinkClass != "" {
		policy.AllowAttrs("class").Matching(regexp.MustCompile("^" + regexp.QuoteMeta(c.LinkClass) + "$")).OnElements("a")
	}
	policy.AllowAttrs("data-blog-cta").Matching(regexp.MustCompile("^true$")).OnElements("a")
	policy.AllowAttrs("aria-hidden").Matching(regexp.MustCompile("^true$")).OnElements("span")
	return policy.Sanitize
}

// articleMarkdownPresentation mirrors the public Markdown reader's heading and
// standalone CTA plugins before sanitization. It never widens allowed targets.
// MarkdownPresentation produces unique heading IDs and configured standalone CTAs.
func MarkdownPresentation(c MarkdownConfig) func(string) (string, error) {
	return func(body string) (string, error) {
		doc, err := xhtml.Parse(strings.NewReader(body))
		if err != nil {
			return "", err
		}
		root := articleNode(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "body" })
		used := map[string]bool{}
		var visit func(*xhtml.Node)
		visit = func(node *xhtml.Node) {
			if node.Type == xhtml.ElementNode && len(node.Data) == 2 && node.Data[0] == 'h' && node.Data[1] >= '1' && node.Data[1] <= '6' {
				base := articleHeadingSlug(articleNodeText(node))
				id := base
				for suffix := 2; used[id]; suffix++ {
					id = base + "-" + strconv.Itoa(suffix)
				}
				used[id] = true
				setArticleAttribute(node, "id", id)
			}
			if node.Type == xhtml.ElementNode && node.Data == "a" && c.CTATitle != "" && articleAttr(node, "title") == c.CTATitle {
				if paragraph := node.Parent; paragraph != nil && paragraph.Data == "p" && paragraph.Parent == root && standaloneArticleLink(paragraph, node) && c.AllowTarget != nil && c.AllowTarget(articleAttr(node, "href")) && articleLinkHasLabel(node) {
					setArticleAttribute(paragraph, "class", c.ParagraphClass)
					setArticleAttribute(node, "class", c.LinkClass)
					setArticleAttribute(node, "data-blog-cta", "true")
					node.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: " "})
					arrow := &xhtml.Node{Type: xhtml.ElementNode, Data: "span", DataAtom: atom.Span, Attr: []xhtml.Attribute{{Key: "aria-hidden", Val: "true"}}}
					arrow.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: c.Arrow})
					node.AppendChild(arrow)
				}
				removeArticleAttribute(node, "title")
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(root)
		var output bytes.Buffer
		for node := root.FirstChild; node != nil; node = node.NextSibling {
			if err := xhtml.Render(&output, node); err != nil {
				return "", err
			}
		}
		return output.String(), nil
	}
}
func articleHeadingSlug(value string) string {
	var result strings.Builder
	dash := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			result.WriteRune(r)
			dash = false
		case r == '-' || unicode.IsSpace(r) || r == '\uFEFF':
			if !dash {
				result.WriteByte('-')
				dash = true
			}
		}
	}
	slug := strings.Trim(result.String(), "-")
	if slug == "" {
		return "section"
	}
	return slug
}
func articleNodeText(node *xhtml.Node) string {
	var text strings.Builder
	var visit func(*xhtml.Node)
	visit = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			text.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return text.String()
}
func standaloneArticleLink(paragraph, link *xhtml.Node) bool {
	for node := paragraph.FirstChild; node != nil; node = node.NextSibling {
		if node == link {
			continue
		}
		if node.Type != xhtml.TextNode || strings.TrimSpace(node.Data) != "" {
			return false
		}
	}
	return true
}
func articleLinkHasLabel(link *xhtml.Node) bool {
	for child := link.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.TextNode && strings.TrimSpace(child.Data) != "" {
			return true
		}
	}
	return false
}
func setArticleAttribute(node *xhtml.Node, key, value string) {
	for i := range node.Attr {
		if node.Attr[i].Key == key {
			node.Attr[i].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, xhtml.Attribute{Key: key, Val: value})
}
func removeArticleAttribute(node *xhtml.Node, key string) {
	for i := 0; i < len(node.Attr); i++ {
		if node.Attr[i].Key == key {
			node.Attr = append(node.Attr[:i], node.Attr[i+1:]...)
			i--
		}
	}
}
