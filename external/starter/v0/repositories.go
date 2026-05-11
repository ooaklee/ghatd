package starter

import (
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/repository"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// Repositories groups the standard GHATD Mongo-backed repositories.
type Repositories struct {
	Core      *repository.MongoDbRepository
	APIToken  *apitoken.Repository
	Audit     *audit.Repository
	Billing   *billing.Repository
	Contacter *contacter.Repository
	Group     *group.Repository
	Notifier  *notifier.Repository
	Post      *post.Repository
	Pricer    *pricer.Repository
	User      *userv2.Repository
}

// NewRepositoriesRequest holds the dependencies and optional overrides for
// repository construction. When a package repository is nil, starter/v0 builds
// it from Core.
type NewRepositoriesRequest struct {
	Core      *repository.MongoDbRepository
	APIToken  *apitoken.Repository
	Audit     *audit.Repository
	Billing   *billing.Repository
	Contacter *contacter.Repository
	Group     *group.Repository
	Notifier  *notifier.Repository
	Post      *post.Repository
	Pricer    *pricer.Repository
	User      *userv2.Repository
}

// NewRepositories creates the standard repository container. Core is required;
// every other repository may be supplied as an override.
func NewRepositories(r *NewRepositoriesRequest) (*Repositories, error) {
	if r == nil {
		return nil, ErrNilRepositoriesRequest
	}
	if r.Core == nil {
		return nil, ErrNilCoreRepository
	}

	repos := &Repositories{
		Core:      r.Core,
		APIToken:  r.APIToken,
		Audit:     r.Audit,
		Billing:   r.Billing,
		Contacter: r.Contacter,
		Group:     r.Group,
		Notifier:  r.Notifier,
		Post:      r.Post,
		Pricer:    r.Pricer,
		User:      r.User,
	}

	if repos.APIToken == nil {
		repos.APIToken = apitoken.NewRepository(r.Core)
	}
	if repos.Audit == nil {
		repos.Audit = audit.NewRepository(r.Core)
	}
	if repos.Billing == nil {
		repos.Billing = billing.NewRepository(r.Core)
	}
	if repos.Contacter == nil {
		repos.Contacter = contacter.NewRepository(r.Core)
	}
	if repos.Group == nil {
		repos.Group = group.NewRepository(r.Core)
	}
	if repos.Notifier == nil {
		repos.Notifier = notifier.NewRepository(r.Core)
	}
	if repos.Post == nil {
		repos.Post = post.NewRepository(r.Core)
	}
	if repos.Pricer == nil {
		repos.Pricer = pricer.NewRepository(r.Core)
	}
	if repos.User == nil {
		repos.User = userv2.NewRepository(r.Core)
	}

	return repos, nil
}

// validateRepositoriesForServices ensures every required repository is present for service construction.
func validateRepositoriesForServices(repos *Repositories) error {
	if repos == nil {
		return ErrNilRepositories
	}
	if repos.APIToken == nil ||
		repos.Audit == nil ||
		repos.Billing == nil ||
		repos.Contacter == nil ||
		repos.Group == nil ||
		repos.Notifier == nil ||
		repos.Post == nil ||
		repos.Pricer == nil ||
		repos.User == nil {
		return ErrNilRepositories
	}

	return nil
}
