package publication

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	xhtml "golang.org/x/net/html"
)

var testMarkdownConfig = MarkdownConfig{CTATitle: "landing-button", ParagraphClass: "blog-article-cta", LinkClass: "landing-button", Arrow: "↗", AllowTarget: func(s string) bool { return s == "/app" || s == "/app/new" || s == "/app/new?kind=money" }}

func articleMarkdownFixture(t *testing.T, markdown string) string {
	t.Helper()
	var output bytes.Buffer
	require.NoError(t, publicArticleMarkdown.Convert([]byte(markdown), &output))
	rendered, err := MarkdownPresentation(testMarkdownConfig)(output.String())
	require.NoError(t, err)
	return MarkdownSanitizer(testMarkdownConfig)(rendered)
}
func TestPublicArticleMarkdownHeadingIDs(t *testing.T) {
	type testCase struct {
		name, markdown string
		want           []string
	}
	for _, tc := range []testCase{
		{"punctuation", "## What's the deal? (money & friends)", []string{"whats-the-deal-money-friends"}},
		{"duplicates across all heading levels", "# Getting paid\n\n## Getting paid\n\n## Getting paid", []string{"getting-paid", "getting-paid-2", "getting-paid-3"}},
		{"whitespace and dashes", "## A -- B   C", []string{"a-b-c"}},
		{"empty slug fallback", "## ✨\n\n## ✨", []string{"section", "section-2"}},
		{"inline formatting", "## A **clear** plan and `date`", []string{"a-clear-plan-and-date"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := xhtml.Parse(strings.NewReader(articleMarkdownFixture(t, tc.markdown)))
			require.NoError(t, err)
			ids := []string{}
			var walk func(*xhtml.Node)
			walk = func(n *xhtml.Node) {
				if n.Type == xhtml.ElementNode && len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
					ids = append(ids, articleAttr(n, "id"))
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(doc)
			require.Equal(t, tc.want, ids)
		})
	}
}
func TestPublicArticleMarkdownCTABoundary(t *testing.T) {
	type testCase struct {
		name, markdown string
		want           bool
	}
	for _, tc := range []testCase{
		{"app", "[Get started](/app \"landing-button\")", true},
		{"new agreement", "[Record details](/app/new \"landing-button\")", true},
		{"supported kind", "[Record money](/app/new?kind=money \"landing-button\")", true},
		{"readable mixed label", "[Start **now**](/app \"landing-button\")", true},
		{"inline link", "Read then [start](/app \"landing-button\") when ready.", false},
		{"quoted example", "> [Example](/app \"landing-button\")", false},
		{"list example", "- [Example](/app \"landing-button\")", false},
		{"foreign URL", "[Example](https://example.test/app \"landing-button\")", false},
		{"unsupported kind", "[Example](/app/new?kind=promise \"landing-button\")", false},
		{"extra query", "[Example](/app/new?kind=money&redirect=elsewhere \"landing-button\")", false},
		{"image label", "[![Example](/image.png)](/app \"landing-button\")", false},
		{"format-only label", "[**Example**](/app \"landing-button\")", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := articleMarkdownFixture(t, tc.markdown)
			require.NotContains(t, body, `title="landing-button"`)
			require.Equal(t, tc.want, strings.Contains(body, `class="blog-article-cta"`))
			require.Equal(t, tc.want, strings.Contains(body, `class="landing-button"`))
			require.Equal(t, tc.want, strings.Contains(body, `data-blog-cta="true"`))
			if tc.want {
				require.Contains(t, body, `aria-hidden="true">↗</span>`)
			}
		})
	}
}
func TestPublicArticlePresentationSanitizerRejectsArbitraryFeatures(t *testing.T) {
	type testCase struct{ name, body string }
	for _, tc := range []testCase{{"style and event", `<p class="cover-screen" style="position:fixed"><a class="landing-button other" data-blog-cta="false" onclick="SECRET()" href="javascript:SECRET()">Example</a></p>`}, {"heading event handler", `<h2 id="section" onclick="SECRET()">Heading</h2>`}} {
		t.Run(tc.name, func(t *testing.T) {
			body := MarkdownSanitizer(testMarkdownConfig)(tc.body)
			require.NotContains(t, body, "SECRET")
			require.NotContains(t, body, "cover-screen")
			require.NotContains(t, body, "style=")
			require.NotContains(t, body, "onclick=")
			require.NotContains(t, body, `data-blog-cta="false"`)
		})
	}
}
