package blueprint

// blueprintRespository expected methods of a valid blueprint repository
type blueprintRespository interface {
}

// Service holds and manages blueprint business logic
type Service struct {
	blueprintRespository blueprintRespository
}

// NewService created blueprint service
func NewService(blueprintRespository blueprintRespository) *Service {
	return &Service{
		blueprintRespository: blueprintRespository,
	}
}
