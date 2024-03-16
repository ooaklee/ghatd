package validator

import (
	validator "github.com/go-playground/validator/v10"
)

// Validator defines custom validator
type Validator struct {
	validator *validator.Validate
}

// New creates a new validator
func NewValidator() *Validator {
	return &Validator{
		validator: createValidator(),
	}

}

// validate makes sure passed struct meets validation rules
func (v *Validator) Validate(s interface{}) error {
	return v.validator.Struct(s)
}

func createValidator() *validator.Validate {

	v := validator.New(validator.WithRequiredStructEnabled())

	return v
}
