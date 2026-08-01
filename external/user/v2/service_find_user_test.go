package user

import (
	"context"
	"errors"
	"testing"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type findUserRepositoryStub struct {
	user          *UniversalUser
	err           error
	email         string
	logErrorValue bool
}

func (*findUserRepositoryStub) CreateUser(context.Context, *UniversalUser) (*UniversalUser, error) {
	return nil, errors.New("not implemented")
}

func (*findUserRepositoryStub) GetUserByID(context.Context, string) (*UniversalUser, error) {
	return nil, errors.New("not implemented")
}

func (*findUserRepositoryStub) GetUserByNanoID(context.Context, string) (*UniversalUser, error) {
	return nil, errors.New("not implemented")
}

func (r *findUserRepositoryStub) GetUserByEmail(_ context.Context, email string, logError bool) (*UniversalUser, error) {
	r.email = email
	r.logErrorValue = logError
	return r.user, r.err
}

func (*findUserRepositoryStub) UpdateUser(context.Context, *UniversalUser) (*UniversalUser, error) {
	return nil, errors.New("not implemented")
}

func (*findUserRepositoryStub) DeleteUserByID(context.Context, string) error {
	return errors.New("not implemented")
}

func (*findUserRepositoryStub) GetUsers(context.Context, *GetUsersRequest) ([]UniversalUser, error) {
	return nil, errors.New("not implemented")
}

func (*findUserRepositoryStub) GetTotalUsers(context.Context, *GetTotalUsersRequest) (int64, error) {
	return 0, errors.New("not implemented")
}

func (*findUserRepositoryStub) GetUserStatsCounts(context.Context, *GetUserStatsRequest) (*UserStats, error) {
	return nil, errors.New("not implemented")
}

func newObservedUserService(repository UserRepository) (*Service, context.Context, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := ghatdlogger.TransitWith(context.Background(), zap.New(core))
	return NewService(repository, nil, nil, nil, nil, nil, ""), ctx, logs
}

func TestFindUserByEmailReturnsExpectedAbsenceWithoutLogging(t *testing.T) {
	repository := &findUserRepositoryStub{err: ErrUserNotFound}
	service, ctx, logs := newObservedUserService(repository)

	response, err := service.FindUserByEmail(ctx, &GetUserByEmailRequest{Email: "Missing@Example.com"})

	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("FindUserByEmail() error = %v, want ErrUserNotFound", err)
	}
	if response != nil {
		t.Fatalf("FindUserByEmail() response = %#v, want nil", response)
	}
	if repository.logErrorValue {
		t.Fatal("FindUserByEmail() enabled repository not-found logging")
	}
	if repository.email != "missing@example.com" {
		t.Fatalf("repository email = %q, want normalized email", repository.email)
	}
	if logs.Len() != 0 {
		t.Fatalf("expected-absence logs = %d, want none", logs.Len())
	}
}

func TestFindUserByEmailReportsDatabaseFailureOnce(t *testing.T) {
	repository := &findUserRepositoryStub{err: errors.New("database unavailable")}
	service, ctx, logs := newObservedUserService(repository)

	response, err := service.FindUserByEmail(ctx, &GetUserByEmailRequest{Email: "user@example.com"})

	if !errors.Is(err, ErrDatabaseError) {
		t.Fatalf("FindUserByEmail() error = %v, want ErrDatabaseError", err)
	}
	if response != nil {
		t.Fatalf("FindUserByEmail() response = %#v, want nil", response)
	}
	if logs.FilterMessage("failed to find user by email").Len() != 1 {
		t.Fatalf("database failure logs = %d, want 1", logs.FilterMessage("failed to find user by email").Len())
	}
}

func TestFindUserByEmailReturnsMatchingUser(t *testing.T) {
	want := &UniversalUser{ID: "user-1", Email: "user@example.com"}
	repository := &findUserRepositoryStub{user: want}
	service, ctx, logs := newObservedUserService(repository)

	response, err := service.FindUserByEmail(ctx, &GetUserByEmailRequest{Email: want.Email})

	if err != nil {
		t.Fatalf("FindUserByEmail() error = %v", err)
	}
	if response == nil || response.User != want {
		t.Fatalf("FindUserByEmail() response = %#v, want user %s", response, want.ID)
	}
	if logs.Len() != 0 {
		t.Fatalf("successful lookup logs = %d, want none", logs.Len())
	}
}

func TestGetUserByEmailRetainsStrictLookupSemantics(t *testing.T) {
	repository := &findUserRepositoryStub{err: ErrUserNotFound}
	service, ctx, logs := newObservedUserService(repository)

	response, err := service.GetUserByEmail(ctx, &GetUserByEmailRequest{Email: "missing@example.com"})

	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUserByEmail() error = %v, want ErrUserNotFound", err)
	}
	if response != nil {
		t.Fatalf("GetUserByEmail() response = %#v, want nil", response)
	}
	if !repository.logErrorValue {
		t.Fatal("GetUserByEmail() did not retain strict repository logging")
	}
	if logs.FilterMessage("failed to get user by email").Len() != 1 {
		t.Fatalf("strict lookup logs = %d, want 1", logs.FilterMessage("failed to get user by email").Len())
	}
}
