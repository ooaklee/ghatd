package user

import "context"

// lookupEmail centralizes strict/quiet read semantics without collapsing native
// error trees or logging private mailbox/adapter values. The repository owns
// optional strict absence diagnostics. Returned models are detached before
// dependency hydration; arbitrary nested extension values remain read-only.
func (s *Service) lookupEmail(ctx context.Context, req *GetUserByEmailRequest, strict bool) (*GetUserByEmailResponse, error) {
	if req == nil || normaliseUserEmail(req.Email) == "" {
		return nil, ErrInvalidEmail
	}
	if ctx == nil || s == nil || nilUserDependency(s.UserRepository) {
		return nil, ErrDatabaseError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	email := normaliseUserEmail(req.Email)
	value, err := s.UserRepository.GetUserByEmail(ctx, email, strict)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if value == nil || value.ID == "" || value.Email != email || value.EmailRevision < 0 {
		return nil, ErrDatabaseError
	}
	detached := copyUserForUpdate(value)
	s.setUserDependencies(detached)
	return &GetUserByEmailResponse{User: detached}, nil
}
