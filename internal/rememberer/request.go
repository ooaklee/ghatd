package rememberer

// GetWordsRequest holds all the data needed to get all words request
type GetWordsRequest struct {
}

// GetWordByIdRequest holds everything needed for getting word based on Id
type GetWordByIdRequest struct {
	// Id the word indentifier
	Id string `validate:"uuid4"`
}

// CreateWordRequest holds everything needed to create word on platform
type CreateWordRequest struct {

	// Name the word the user wishes to add to service
	Name string `json:"name" validate:"required,min=3"`
}

// DeleteWordRequest holds everything needed for deleting a word request
type DeleteWordRequest struct {
	// Id the word's UUId
	Id string `validate:"uuid4"`
}
