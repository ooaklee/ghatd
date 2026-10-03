package post

import (
	"context"
	"testing"
)

// postTargetProbe fails if the service chooses any repository operation other
// than looking up the explicitly agreed target.
type postTargetProbe struct {
	contenterRepository
	ids []string
}

func (p *postTargetProbe) GetPostById(_ context.Context, id string) (*Post, error) {
	p.ids = append(p.ids, id)
	return nil, ErrResourceNotFound
}

func TestPostReplacementTargetBinding(t *testing.T) {
	for _, tc := range []struct {
		name, target, replacement string
		nilRequest, nilContext    bool
		want                      error
		lookup                    string
	}{
		{name: "nil request", nilRequest: true, want: ErrPostBadRequest},
		{name: "nil context", nilContext: true, want: ErrPostBadRequest},
		{name: "contradictory targets", target: "selected", replacement: "other", want: ErrPostBadRequest},
		{name: "same target", target: "selected", replacement: "selected", want: ErrPostNotFoundForUpdate, lookup: "selected"},
		{name: "trusted replacement only", replacement: "selected", want: ErrPostNotFoundForUpdate, lookup: "selected"},
		{name: "field update target", target: "selected", want: ErrPostNotFoundForUpdate, lookup: "selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := &postTargetProbe{}
			req := &UpdatePostRequest{ActorID: "actor", PostId: tc.target}
			if tc.replacement != "" {
				req.Post = &Post{Id: tc.replacement}
			}
			if tc.nilRequest {
				req = nil
			}
			var ctx context.Context = context.Background()
			if tc.nilContext {
				ctx = nil
			}
			_, err := NewService(port, nil).UpdatePost(ctx, req)
			if err != tc.want {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if tc.lookup == "" {
				if len(port.ids) != 0 {
					t.Fatalf("unexpected lookup %v", port.ids)
				}
			} else if len(port.ids) != 1 || port.ids[0] != tc.lookup {
				t.Fatalf("target lookups=%v", port.ids)
			}
		})
	}
}
