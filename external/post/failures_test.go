package post

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// absenceImpostor must not gain create permission through custom Is matching.
type absenceImpostor struct{}

func (absenceImpostor) Error() string { return "private outage" }
func (absenceImpostor) Is(error) bool { return true }

type loopingPostError struct{}

func (*loopingPostError) Error() string   { return "private cycle" }
func (e *loopingPostError) Unwrap() error { return e }

type uncomparablePostError []int

func (uncomparablePostError) Error() string { return "private slice" }

func TestPostAbsenceEvidence(t *testing.T) {
	deep := error(ErrResourceNotFound)
	for i := 0; i < 64; i++ {
		deep = fmt.Errorf("layer: %w", deep)
	}
	for _, tc := range []struct {
		name   string
		err    error
		absent bool
	}{
		{"nil", nil, false}, {"domain", ErrResourceNotFound, true}, {"mongo", mongo.ErrNoDocuments, true},
		{"wrapped", fmt.Errorf("read: %w", ErrResourceNotFound), true},
		{"wrapped mongo", fmt.Errorf("read: %w", mongo.ErrNoDocuments), true},
		{"same message", errors.New(mongo.ErrNoDocuments.Error()), false},
		{"alias", absenceImpostor{}, false}, {"singleton join", errors.Join(ErrResourceNotFound), false},
		{"mixed", errors.Join(ErrResourceNotFound, context.DeadlineExceeded), false},
		{"cycle", &loopingPostError{}, false}, {"typed nil", (*loopingPostError)(nil), false},
		{"uncomparable", uncomparablePostError{1}, false}, {"bounded", deep, false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.absent, postAbsent(tc.err)) })
	}
}

// postFailurePort records domain dispatch; unimplemented methods deliberately
// panic so a wrong operation cannot accidentally make a test pass.
type postFailurePort struct {
	contenterRepository
	item, slug, receipt                             *Post
	lookupErr, slugErr, writeErr, countErr, listErr error
	cancelLookup, cancelSlug, cancelWrite           context.CancelFunc
	writes, reads, slugReads, counts, lists         int
	nilReceipt, mutate                              bool
	query                                           *GetPostsRequest
	countQuery                                      *GetTotalPostsRequest
	total                                           int64
	rows                                            []Post
}

func (p *postFailurePort) GetPostById(context.Context, string) (*Post, error) {
	p.reads++
	if p.cancelLookup != nil {
		p.cancelLookup()
	}
	return p.item, p.lookupErr
}
func (p *postFailurePort) GetPostByUrlFriendlyId(context.Context, string) (*Post, error) {
	p.slugReads++
	if p.cancelSlug != nil {
		p.cancelSlug()
	}
	return p.slug, p.slugErr
}
func (p *postFailurePort) save(item *Post) (*Post, error) {
	p.writes++
	if p.cancelWrite != nil {
		p.cancelWrite()
	}
	if p.mutate {
		item.Text = "adapter-mutated"
		if len(item.Tags) > 0 {
			item.Tags[0] = "adapter-mutated"
		}
		if len(item.Visibility) > 0 {
			item.Visibility[0] = "adapter-mutated"
		}
	}
	if p.writeErr != nil {
		return nil, p.writeErr
	}
	if p.nilReceipt {
		return nil, nil
	}
	if p.receipt != nil {
		return p.receipt, nil
	}
	copy := copyPost(item)
	if copy.Id == "" {
		copy.Id = "target"
	}
	return copy, nil
}
func (p *postFailurePort) CreatePost(_ context.Context, item *Post) (*Post, error) {
	return p.save(item)
}
func (p *postFailurePort) UpdatePost(_ context.Context, item *Post) (*Post, error) {
	return p.save(item)
}
func (p *postFailurePort) DeletePost(context.Context, string) error {
	_, err := p.save(&Post{})
	return err
}
func (p *postFailurePort) SoftDeletePost(_ context.Context, item *Post, _ string) error {
	_, err := p.save(item)
	return err
}
func (p *postFailurePort) GetTotalPosts(_ context.Context, q *GetTotalPostsRequest) (int64, error) {
	p.counts++
	p.countQuery = q
	return p.total, p.countErr
}
func (p *postFailurePort) GetPosts(_ context.Context, q *GetPostsRequest) ([]Post, error) {
	p.lists++
	p.query = q
	return p.rows, p.listErr
}

// validPostSnapshot includes mutable fields to detect aliasing on failed writes.
func validPostSnapshot() *Post {
	return &Post{Id: "target", Title: "Original", Text: "private body", Type: PostTypeFaq, UrlFriendlyId: "faq-original", Tags: []string{"original"}, Visibility: []string{"original"}}
}

// invokePostOperation uses real public entries with valid minimal inputs.
func invokePostOperation(s *Service, ctx context.Context, op string, nilRequest bool) error {
	switch op {
	case "create":
		r := &CreatePostRequest{ActorID: "actor", Title: "Original", Text: "private body", Type: PostTypeFaq}
		if nilRequest {
			r = nil
		}
		_, e := s.CreatePost(ctx, r)
		return e
	case "update":
		r := &UpdatePostRequest{ActorID: "actor", PostId: "target"}
		if nilRequest {
			r = nil
		}
		_, e := s.UpdatePost(ctx, r)
		return e
	case "delete", "hard delete":
		r := &DeletePostByIdRequest{ActorID: "actor", Id: "target", HardDelete: op == "hard delete"}
		if nilRequest {
			r = nil
		}
		_, e := s.DeletePostById(ctx, r)
		return e
	case "restore":
		r := &RestorePostByIdRequest{ActorID: "actor", Id: "target"}
		if nilRequest {
			r = nil
		}
		_, e := s.RestorePostById(ctx, r)
		return e
	case "lookup":
		_, e := s.GetPostByUrlFriendlyId(ctx, "faq-original")
		return e
	case "list":
		r := &GetPostsRequest{}
		if nilRequest {
			r = nil
		}
		_, e := s.GetPosts(ctx, r)
		return e
	case "changelog":
		r := &GetChangelogItemsRequest{GetPostsRequest: &GetPostsRequest{}}
		if nilRequest {
			r = nil
		}
		_, e := s.GetChangelogItems(ctx, r)
		return e
	case "glossary":
		r := &GetGlossaryItemsRequest{GetPostsRequest: &GetPostsRequest{}}
		if nilRequest {
			r = nil
		}
		_, e := s.GetGlossaryItems(ctx, r)
		return e
	case "faq":
		r := &GetFaqItemsRequest{GetPostsRequest: &GetPostsRequest{}}
		if nilRequest {
			r = nil
		}
		_, e := s.GetFaqItems(ctx, r)
		return e
	case "articles":
		r := &GetArticlesRequest{GetPostsRequest: &GetPostsRequest{}}
		if nilRequest {
			r = nil
		}
		_, e := s.GetArticles(ctx, r)
		return e
	case "latest":
		r := &GetLatestPostsByTypeRequest{}
		if nilRequest {
			r = nil
		}
		_, e := s.GetLatestPostsByType(ctx, r)
		return e
	case "notifications":
		r := &common.GetLatestNotificationOverviewsRequest{}
		if nilRequest {
			r = nil
		}
		_, e := s.GetLatestNotificationOverviews(ctx, r)
		return e
	default:
		panic("unrecognized operation")
	}
}

func TestPostServiceEntryGuards(t *testing.T) {
	for _, op := range []string{"create", "update", "delete", "restore", "lookup", "list", "changelog", "glossary", "faq", "articles", "latest", "notifications"} {
		for _, tc := range []struct {
			name string
			want error
		}{{"nil context", ErrPostBadRequest}, {"nil request", ErrPostBadRequest}, {"cancelled", context.Canceled}, {"nil service", ErrPostUnavailable}, {"nil port", ErrPostUnavailable}, {"typed nil port", ErrPostUnavailable}} {
			if op == "lookup" && tc.name == "nil request" {
				continue
			} // String selectors have no nil value; blank selectors are tested separately.
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				p := &postFailurePort{}
				s := NewService(p, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch tc.name {
				case "nil context":
					ctx = nil
				case "cancelled":
					cancel()
				case "nil service":
					s = nil
				case "nil port":
					s.contenterRepository = nil
				case "typed nil port":
					s.contenterRepository = (*postFailurePort)(nil)
				}
				require.Equal(t, tc.want, invokePostOperation(s, ctx, op, tc.name == "nil request"))
				require.Zero(t, p.reads+p.writes+p.slugReads+p.counts+p.lists)
			})
		}
	}
}

func TestPostLookupFailurePropagation(t *testing.T) {
	outage := errors.New("private storage diagnostic")
	wrapped := fmt.Errorf("private diagnostic: %w", outage)
	for _, op := range []string{"update", "delete", "hard delete", "restore"} {
		for _, tc := range []struct {
			name    string
			failure error
			missing bool
		}{
			{"domain absence", ErrResourceNotFound, true}, {"mongo absence", mongo.ErrNoDocuments, true}, {"wrapped absence", fmt.Errorf("read: %w", ErrResourceNotFound), true},
			{"outage", outage, false}, {"wrapped outage", wrapped, false}, {"mixed", errors.Join(ErrResourceNotFound, outage), false}, {"singleton join", errors.Join(ErrResourceNotFound), false},
			{"cancellation", context.Canceled, false}, {"deadline", context.DeadlineExceeded, false},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				p := &postFailurePort{lookupErr: tc.failure}
				err := invokePostOperation(NewService(p, nil), context.Background(), op, false)
				want := tc.failure
				if tc.missing {
					want = ErrResourceNotFound
					if op == "update" {
						want = ErrPostNotFoundForUpdate
					}
				}
				require.True(t, want == err, "original error identity must be preserved")
				require.Equal(t, 1, p.reads)
				require.Zero(t, p.writes+p.slugReads)
			})
		}
	}
}

func TestPostSlugFailureAdmission(t *testing.T) {
	outage := errors.New("private slug outage")
	for _, op := range []string{"create", "field update", "replacement"} {
		for _, tc := range []struct {
			name    string
			failure error
			owner   string
			want    error
			write   bool
		}{
			{"absent", ErrResourceNotFound, "", nil, true}, {"wrapped absent", fmt.Errorf("read: %w", mongo.ErrNoDocuments), "", nil, true},
			{"outage", outage, "", outage, false}, {"alias", absenceImpostor{}, "", absenceImpostor{}, false},
			{"nil result", nil, "", ErrPostUnavailable, false}, {"other owner", nil, "other", ErrPostAlreadyExistsWithGivenUrlFriendlyId, false},
			{"self", nil, "target", nil, true},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				p := &postFailurePort{item: validPostSnapshot(), slugErr: tc.failure}
				if tc.owner != "" {
					p.slug = &Post{Id: tc.owner, UrlFriendlyId: "faq-new"}
				}
				s := NewService(p, nil)
				var err error
				if op == "create" {
					_, err = s.CreatePost(context.Background(), &CreatePostRequest{ActorID: "actor", Title: "New", Text: "body", Type: PostTypeFaq})
				} else {
					title := "New"
					r := &UpdatePostRequest{ActorID: "actor", PostId: "target", Title: &title}
					if op == "replacement" {
						r.Post = validPostSnapshot()
						r.Post.Title = title
					}
					_, err = s.UpdatePost(context.Background(), r)
				}
				want, write := tc.want, tc.write
				if op == "create" && tc.name == "self" {
					want = ErrPostAlreadyExistsWithGivenUrlFriendlyId
					write = false
				}
				require.Equal(t, want, err)
				require.Equal(t, 1, p.slugReads)
				require.Equal(t, map[bool]int{true: 1, false: 0}[write], p.writes)
			})
		}
	}
}

func TestPostMutationSnapshotsAndReceipts(t *testing.T) {
	for _, op := range []string{"update", "replacement", "delete", "restore"} {
		for _, tc := range []struct {
			name    string
			failure error
		}{{"success", nil}, {"write failure", errors.New("private write diagnostic")}} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				original := validPostSnapshot()
				if op == "restore" {
					original.DeletedAt = "deleted"
					original.PublishedAt = "published"
				}
				before := copyPost(original)
				p := &postFailurePort{item: original, writeErr: tc.failure, mutate: true}
				s := NewService(p, nil)
				var err error
				if op == "replacement" {
					_, err = s.UpdatePost(context.Background(), &UpdatePostRequest{ActorID: "actor", Post: original})
				} else {
					err = invokePostOperation(s, context.Background(), op, false)
				}
				require.Equal(t, tc.failure, err)
				require.Equal(t, before, original)
				require.Equal(t, 1, p.writes)
			})
		}
	}
	for _, op := range []string{"create", "update", "restore"} {
		for _, kind := range []string{"nil receipt", "wrong identity", "late cancellation"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p := &postFailurePort{item: validPostSnapshot(), slugErr: ErrResourceNotFound}
				p.item.DeletedAt = "deleted"
				if op == "update" {
					p.item.DeletedAt = ""
				}
				want := ErrPostUnavailable
				switch kind {
				case "nil receipt":
					p.nilReceipt = true
				case "wrong identity":
					p.receipt = &Post{Id: "wrong"}
				case "late cancellation":
					p.cancelWrite = cancel
					want = nil
				}
				require.Equal(t, want, invokePostOperation(NewService(p, nil), ctx, op, false))
				require.Equal(t, 1, p.writes)
			})
		}
	}
}

func TestPostSlugRegeneration(t *testing.T) {
	for _, mode := range []string{"fields", "replacement"} {
		for _, tc := range []struct {
			name, title string
			kind        PostType
			slug        string
			checks      int
		}{
			{"neither", "Original", PostTypeFaq, "faq-original", 0}, {"title", "New", PostTypeFaq, "faq-new", 1}, {"type", "Original", PostTypeGlossary, "glossary-original", 1}, {"both", "New", PostTypeGlossary, "glossary-new", 1},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				p := &postFailurePort{item: validPostSnapshot(), slugErr: ErrResourceNotFound}
				r := &UpdatePostRequest{ActorID: "actor", PostId: "target", Title: &tc.title, Type: &tc.kind}
				if mode == "replacement" {
					r.Post = validPostSnapshot()
					r.Post.Title = tc.title
					r.Post.Type = tc.kind
				}
				result, err := NewService(p, nil).UpdatePost(context.Background(), r)
				require.NoError(t, err)
				require.Equal(t, tc.slug, result.Post.UrlFriendlyId)
				require.Equal(t, tc.checks, p.slugReads)
			})
		}
	}
}

func TestPostReadRequestIsolation(t *testing.T) {
	for _, kind := range []string{"list", "changelog", "glossary", "faq", "articles"} {
		t.Run(kind, func(t *testing.T) {
			p := &postFailurePort{}
			s := NewService(p, nil)
			q := &GetPostsRequest{WithTypes: "other", WithTextFormats: "markdown", WithHeaderImageType: "svg"}
			original := *q
			var err error
			ctx := context.Background()
			switch kind {
			case "list":
				_, err = s.GetPosts(ctx, q)
			case "changelog":
				_, err = s.GetChangelogItems(ctx, &GetChangelogItemsRequest{q})
			case "glossary":
				_, err = s.GetGlossaryItems(ctx, &GetGlossaryItemsRequest{q})
			case "faq":
				_, err = s.GetFaqItems(ctx, &GetFaqItemsRequest{q})
			case "articles":
				_, err = s.GetArticles(ctx, &GetArticlesRequest{q})
			}
			require.NoError(t, err)
			require.Equal(t, original, *q)
			require.Equal(t, []HeaderImageType{HeaderImageTypeSvg}, p.countQuery.PostHeaderImageTypes)
			require.Equal(t, 1, p.query.Page)
			require.Equal(t, 25, p.query.PerPage)
		})
	}
}

func TestPostLogsExcludePrivateDiagnostics(t *testing.T) {
	for _, op := range []string{"create", "update", "delete", "restore", "list"} {
		t.Run(op, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(context.Background(), zap.New(core))
			outage := errors.New("private storage diagnostic")
			p := &postFailurePort{lookupErr: outage, slugErr: outage, countErr: outage}
			require.Same(t, outage, invokePostOperation(NewService(p, nil), ctx, op, false))
			for _, entry := range logs.All() {
				require.NotContains(t, entry.Message, "private")
				require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private")
			}
		})
	}
}

// Ensure reflection-based nil checks accept ordinary non-pointer adapters too.
func TestPostDependencyNilKinds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  bool
	}{{"nil", nil, true}, {"typed pointer", (*postFailurePort)(nil), true}, {"value", struct{}{}, false}, {"zero int", 0, false}, {"slice", []int(nil), true}} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, nilPostDependency(tc.value))
			if tc.value != nil {
				require.NotEqual(t, reflect.Invalid, reflect.ValueOf(tc.value).Kind())
			}
		})
	}
}

func TestPostLookupResultsAndCancellation(t *testing.T) {
	for _, op := range []string{"update", "delete", "restore"} {
		for _, kind := range []string{"nil result", "wrong target", "cancelled lookup"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p := &postFailurePort{item: validPostSnapshot()}
				want := ErrPostUnavailable
				switch kind {
				case "nil result":
					p.item = nil
				case "wrong target":
					p.item.Id = "other"
				case "cancelled lookup":
					p.cancelLookup = cancel
					want = context.Canceled
				}
				require.Equal(t, want, invokePostOperation(NewService(p, nil), ctx, op, false))
				require.Zero(t, p.writes+p.slugReads)
			})
		}
	}
	for _, op := range []string{"create", "update"} {
		t.Run(op+"/cancelled slug", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &postFailurePort{item: validPostSnapshot(), slugErr: ErrResourceNotFound, cancelSlug: cancel}
			s := NewService(p, nil)
			var err error
			if op == "create" {
				err = invokePostOperation(s, ctx, op, false)
			} else {
				title := "new"
				_, err = s.UpdatePost(ctx, &UpdatePostRequest{ActorID: "actor", PostId: "target", Title: &title})
			}
			require.Equal(t, context.Canceled, err)
			require.Zero(t, p.writes)
		})
	}
}

func TestPostRequiredInputs(t *testing.T) {
	for _, op := range []string{"create", "update", "delete", "restore"} {
		for _, actor := range []string{"", "   "} {
			t.Run(op+"/actor="+actor, func(t *testing.T) {
				p := &postFailurePort{}
				s := NewService(p, nil)
				ctx := context.Background()
				var err error
				switch op {
				case "create":
					_, err = s.CreatePost(ctx, &CreatePostRequest{ActorID: actor})
				case "update":
					_, err = s.UpdatePost(ctx, &UpdatePostRequest{ActorID: actor, PostId: "target"})
				case "delete":
					_, err = s.DeletePostById(ctx, &DeletePostByIdRequest{ActorID: actor, Id: "target"})
				case "restore":
					_, err = s.RestorePostById(ctx, &RestorePostByIdRequest{ActorID: actor, Id: "target"})
				}
				require.Equal(t, ErrUserIdMustBeProvided, err)
				require.Zero(t, p.reads+p.writes+p.slugReads)
			})
		}
	}
	for _, op := range []string{"changelog", "faq", "glossary", "articles"} {
		t.Run(op+"/nil embedded", func(t *testing.T) {
			p := &postFailurePort{}
			s := NewService(p, nil)
			ctx := context.Background()
			var err error
			switch op {
			case "changelog":
				_, err = s.GetChangelogItems(ctx, &GetChangelogItemsRequest{})
			case "faq":
				_, err = s.GetFaqItems(ctx, &GetFaqItemsRequest{})
			case "glossary":
				_, err = s.GetGlossaryItems(ctx, &GetGlossaryItemsRequest{})
			case "articles":
				_, err = s.GetArticles(ctx, &GetArticlesRequest{})
			}
			require.Equal(t, ErrPostBadRequest, err)
			require.Zero(t, p.counts+p.lists)
		})
	}
}

func TestPostDirectReadOutcomes(t *testing.T) {
	outage := errors.New("private read diagnostic")
	for _, tc := range []struct {
		name    string
		failure error
		want    error
	}{
		{"found", nil, nil}, {"missing", ErrResourceNotFound, ErrResourceNotFound}, {"wrapped missing", fmt.Errorf("read: %w", ErrResourceNotFound), ErrResourceNotFound},
		{"outage", outage, outage}, {"same message", errors.New(ErrResourceNotFound.Error()), nil}, {"mixed", errors.Join(ErrResourceNotFound, outage), nil},
		{"nil result", nil, ErrPostUnavailable}, {"wrong slug", nil, ErrPostUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &postFailurePort{slug: validPostSnapshot(), slugErr: tc.failure}
			if tc.name == "nil result" {
				p.slug = nil
			}
			if tc.name == "wrong slug" {
				p.slug.UrlFriendlyId = "other"
			}
			got, err := NewService(p, nil).GetPostByUrlFriendlyId(context.Background(), "faq-original")
			want := tc.want
			if want == nil && tc.failure != nil {
				want = tc.failure
			}
			require.Equal(t, want, err)
			if err != nil {
				require.Nil(t, got)
			} else {
				got.Tags[0] = "changed"
				require.Equal(t, "original", p.slug.Tags[0])
			}
		})
	}
	for _, phase := range []string{"count", "list"} {
		t.Run(phase+" failure", func(t *testing.T) {
			p := &postFailurePort{}
			if phase == "count" {
				p.countErr = outage
			} else {
				p.listErr = outage
			}
			got, err := NewService(p, nil).GetPosts(context.Background(), &GetPostsRequest{})
			require.Same(t, outage, err)
			require.Nil(t, got)
			if phase == "count" {
				require.Zero(t, p.lists)
			}
		})
	}
}
