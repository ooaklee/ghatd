package policy

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// MapRequestToGetPoliciesRequest maps incoming Get Policies request to correct
// struct.
func MapRequestToGetPoliciesRequest(request *http.Request, validator policyValidator) (*GetPoliciesRequest, error) {
	parsedRequest := &GetPoliciesRequest{}

	if err := ValidateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidpolicyName)
	}

	return parsedRequest, nil
}

// MapRequestToGetPolicyByNameRequest maps incoming Get Policy By Name request to correct
// struct.
func MapRequestToGetPolicyByNameRequest(request *http.Request, validator policyValidator) (*GetPolicyByNameRequest, error) {
	parsedRequest := &GetPolicyByNameRequest{}

	policyName, err := GetpolicyNameFromUri(request)
	if err != nil {
		return nil, err
	}

	// standardise policy name in format
	standardisePolicyName := strings.ReplaceAll(
		toolbox.StringStandardisedToLower(policyName),
		" ",
		"-")

	parsedRequest.PolicyName = standardisePolicyName

	if err := ValidateParsedRequest(parsedRequest, validator); err != nil {
		return nil, errors.New(ErrKeyInvalidpolicyName)
	}

	return parsedRequest, nil
}

// ValidateParsedRequest validates based on tags. On failure an error is returned
func ValidateParsedRequest(request interface{}, validator policyValidator) error {
	return validator.Validate(request)
}

// GetpolicyNameFromUri extracts the policy name from the incoming HTTP request.
// It returns the policy name as a string, or an error if the policy name is missing
func GetpolicyNameFromUri(request *http.Request) (string, error) {
	var policyName string

	if policyName = mux.Vars(request)["policyName"]; policyName == "" {
		return "", errors.New(ErrKeyInvalidpolicyName)
	}

	return policyName, nil
}
