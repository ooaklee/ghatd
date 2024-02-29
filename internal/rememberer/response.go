package rememberer

// GetWordsResponse holds all the data requested in the get all words request
type GetWordsResponse struct {
	// Words is the collection of words on the platform
	Words []Word `json:"words"`
}

// GetWordByIdResponse holds the word that matches the on Id requested
type GetWordByIdResponse struct {
	// Word holds the word that matches the passed id
	Word *Word `json:"word"`
}

// CreateWordResponse holds the response for when new word is added to the platform
type CreateWordResponse struct {

	// Word holds created, including its Id
	Word *Word `json:"word"`
}
