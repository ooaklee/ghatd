package billingmanager

import (
	"context"
	"errors"
	"testing"

	user "github.com/ooaklee/ghatd/external/user/v2"
)

type billingOptionalEmailFinderStub struct {
	findCalls int
	getCalls  int
	findError error
}

func (s *billingOptionalEmailFinderStub) GetUserByEmail(context.Context, *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	s.getCalls++
	return nil, errors.New("strict lookup should not be used")
}

func (s *billingOptionalEmailFinderStub) FindUserByEmail(context.Context, *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	s.findCalls++
	return nil, s.findError
}

func (*billingOptionalEmailFinderStub) GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	return nil, errors.New("not implemented")
}

func TestFindUserByEmailUsesExpectedAbsenceCapability(t *testing.T) {
	userService := &billingOptionalEmailFinderStub{findError: user.ErrUserNotFound}

	response, err := findUserByEmail(context.Background(), userService, &user.GetUserByEmailRequest{Email: "missing@example.com"})

	if !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("findUserByEmail() error = %v, want ErrUserNotFound", err)
	}
	if response != nil {
		t.Fatalf("findUserByEmail() response = %#v, want nil", response)
	}
	if userService.findCalls != 1 || userService.getCalls != 0 {
		t.Fatalf("lookup calls = find:%d strict:%d, want find:1 strict:0", userService.findCalls, userService.getCalls)
	}
}
