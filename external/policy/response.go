package policy

// GetPoliciesResponse is the data that will be returned
// when a get policies request is made
type GetPoliciesResponse struct {

	// Policies a list of policies
	Policies []WebAppPolicy `json:"policies"`
}

// GetPolicyByNameResponse is the data that will be returned
// when a get policy by id request is made
type GetPolicyByNameResponse struct {
	Policy WebAppPolicy
}
