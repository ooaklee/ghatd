package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/indexnow"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/seo"
)

// ReconciliationScope preserves the durable version-1 identity and raw policy bytes.
func ReconciliationScope(component, environment, origin string, files fs.FS, prefix string) (string, error) {
	f, err := newPublicSitemapFilter(origin, files, path.Join(prefix, "dist"))
	if err != nil {
		return "", errors.New("indexnow worker publication policy invalid")
	}
	var policy []byte
	if files != nil {
		policy, err = fs.ReadFile(files, path.Join(prefix, "dist", "sitemap-policy.json"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", errors.New("indexnow worker publication policy unavailable")
		}
	}
	encoded, err := json.Marshal(struct {
		Version                        int
		Component, Environment, Origin string
		Policy                         []byte
	}{1, component, environment, strings.ToLower(f.origin.Scheme) + "://" + strings.ToLower(f.origin.Host), policy})
	if err != nil {
		return "", errors.New("indexnow worker scope unavailable")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ReconciliationRuntime borrows the supplied Mongo runtime; the caller closes it.
type ReconciliationRuntime struct {
	CMS  *contentmanager.Service
	Jobs indexnow.JobRepository
}

// NewReconciliationRuntime assembles CMS and SEO sources without starting a worker.
func NewReconciliationRuntime(mongo *repository.MongoRuntime, cfg IndexNowConfig, filter *SitemapFilter) (*ReconciliationRuntime, error) {
	if mongo == nil || mongo.CoreRepository == nil || filter == nil {
		return nil, errors.New("indexnow reconciliation dependencies required")
	}
	if !cfg.Enabled {
		return nil, errors.New("indexnow integration disabled")
	}
	cfg.Attempts = indexnow.NewRepository(mongo.CoreRepository)
	service, err := BuildIndexNow(cfg)
	if err != nil {
		return nil, err
	}
	posts := post.NewRepository(mongo.CoreRepository)
	sitemap := seo.NewRepository(mongo.CoreRepository)
	cms := contentmanager.NewService(post.NewService(posts, post.DefaultValidPostTags), nil, contentmanager.WithIndexNow(service, contentmanager.IndexNowConfig{CanonicalOrigin: cfg.SiteURL, URLForPost: PublicContentURL, LookupPost: posts.GetPostById, AllowURL: filter.AllowIndexNowURL, PublicationSource: posts, SitemapSource: sitemap, EligibleURL: filter.Eligibility(posts.GetPostByUrlFriendlyId)}))
	return &ReconciliationRuntime{CMS: cms, Jobs: indexnow.NewJobRepository(mongo.CoreRepository)}, nil
}

// NewReconciliationRunner constructs a durable leased runner without starting it.
func NewReconciliationRunner(config indexnow.RunnerConfig, runtime *ReconciliationRuntime) (*indexnow.Runner, error) {
	if runtime == nil || runtime.CMS == nil {
		return nil, errors.New("indexnow reconciliation runtime required")
	}
	return indexnow.NewRunner(config, runtime.Jobs, func(ctx context.Context, after, through time.Time, limit int) (bool, error) {
		result, err := runtime.CMS.ReconcileIndexNowEligibility(ctx, &contentmanager.ReconcileIndexNowEligibilityRequest{After: after, Through: through, Limit: limit})
		return result != nil && result.Complete, err
	})
}
