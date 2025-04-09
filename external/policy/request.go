package policy

// GetPoliciesRequest holds everything needed to make
// the request to get policies
type GetPoliciesRequest struct {
}

// GetPolicyByNameRequest holds everything needed to make
// the request to get a policy by its id
type GetPolicyByNameRequest struct {

	// PolicyName the name of the policy to get
	PolicyName string
}
