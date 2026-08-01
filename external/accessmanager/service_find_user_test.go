package accessmanager

import (
	"context"
	"errors"
	"testing"

	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

type optionalEmailFinderStub struct {
	findCalls int
	getCalls  int
	findError error
}

func (*optionalEmailFinderStub) GetUserByNanoID(context.Context, *userv2.GetUserByNanoIDRequest) (*userv2.GetUserByNanoIDResponse, error) {
	return nil, errors.New("not implemented")
}

func (*optionalEmailFinderStub) GetUserByID(context.Context, *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *optionalEmailFinderStub) GetUserByEmail(context.Context, *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error) {
	s.getCalls++
	return nil, errors.New("strict lookup should not be used")
}

func (s *optionalEmailFinderStub) FindUserByEmail(context.Context, *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error) {
	s.findCalls++
	return nil, s.findError
}

func (*optionalEmailFinderStub) UpdateUser(context.Context, *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
	return nil, errors.New("not implemented")
}

func (*optionalEmailFinderStub) CreateUser(context.Context, *userv2.CreateUserRequest) (*userv2.CreateUserResponse, error) {
	return nil, errors.New("not implemented")
}

func TestFindUserByEmailUsesExpectedAbsenceCapability(t *testing.T) {
	userService := &optionalEmailFinderStub{findError: userv2.ErrUserNotFound}

	response, err := findUserByEmail(context.Background(), userService, &userv2.GetUserByEmailRequest{Email: "missing@example.com"})

	if !errors.Is(err, userv2.ErrUserNotFound) {
		t.Fatalf("findUserByEmail() error = %v, want ErrUserNotFound", err)
	}
	if response != nil {
		t.Fatalf("findUserByEmail() response = %#v, want nil", response)
	}
	if userService.findCalls != 1 || userService.getCalls != 0 {
		t.Fatalf("lookup calls = find:%d strict:%d, want find:1 strict:0", userService.findCalls, userService.getCalls)
	}
}
