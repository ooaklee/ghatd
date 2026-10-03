package post_test

import (
	"testing"

	"github.com/go-playground/assert/v2"
	"github.com/ooaklee/ghatd/external/post"
)

func TestModel_GenerateUrlFriendlyId(t *testing.T) {

	tests := []struct {
		name                string
		rawTitle            string
		providedType        string
		expectedUrlFriendly string
	}{
		{
			name:                "Success - Generated url with expected format",
			rawTitle:            "Wrapping up: Q2 2025",
			providedType:        "article",
			expectedUrlFriendly: "article-wrapping-up-q2-2025",
		},
		{
			name:     "trim surrounding spaces and lowercase",
			rawTitle: "  Getting Started  ", providedType: "faq",
			expectedUrlFriendly: "faq-getting-started",
		},
		{
			name:     "type contributes a distinct prefix",
			rawTitle: "Getting Started", providedType: "glossary",
			expectedUrlFriendly: "glossary-getting-started",
		},
		{
			name:     "remove punctuation and preserve digits",
			rawTitle: "Hello, World! 42", providedType: "other",
			expectedUrlFriendly: "other-hello-world-42",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			post := &post.Post{
				Title: test.rawTitle,
				Type:  post.PostType(test.providedType),
			}

			post.GenerateUrlFriendlyId()

			assert.Equal(t, post.UrlFriendlyId, test.expectedUrlFriendly)
		})
	}
}
