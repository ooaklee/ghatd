package user

import (
	"fmt"

	userX "github.com/ooaklee/ghatd/external/user/x"
)

// CastToV1User safely converts a UserModel interface to v1 User struct
// Returns error if the model is not v1 or conversion fails
func CastToV1User(model userX.UserModel) (*User, error) {
	if model == nil {
		return nil, userX.ErrInvalidUserModel
	}

	if model.GetModelVersion() != userX.UserModelVersionV1 {
		return nil, fmt.Errorf("%w: expected v1 but got v%d", userX.ErrUnsupportedUserModelVersion, model.GetModelVersion())
	}

	user, ok := model.(*User)
	if !ok {
		return nil, fmt.Errorf("%w: failed to cast to v1 User", userX.ErrInvalidUserModel)
	}

	return user, nil
}

// CastV1UserSliceToInterfaceSlice converts a slice of v1 User to a slice of UserModel interface
func CastV1UserSliceToInterfaceSlice(users []User) []userX.UserModel {
	result := make([]userX.UserModel, len(users))
	for i, u := range users {
		result[i] = &u
	}
	return result
}

// ConvertToUserModel wraps v1 user into UserModel interface
func ConvertToUserModel(user *User) (userX.UserModel, error) {
	if user == nil {
		return nil, userX.ErrInvalidUserModel
	}
	return user, nil
}

// ConvertUsersToModels converts a slice of v1 users to UserModelSlice
func ConvertUsersToModels(users []*User) (userX.UserModelSlice, error) {
	if users == nil {
		return userX.UserModelSlice{}, nil
	}

	models := make(userX.UserModelSlice, len(users))
	for i, u := range users {
		if u == nil {
			continue
		}
		models[i] = u
	}
	return models, nil
}
